package main

import (
	"context"
	"strings"
	"sync"
	"time"
	"github.com/bondar-aleksandr/netrasp/pkg/netrasp"
	"fmt"
	"os"
	"bufio"
	"path/filepath"
)

type worker struct {
	device *Device
	globalWg *sync.WaitGroup
	localWg *sync.WaitGroup
	ctx context.Context
}

// constructor
func NewWorker(ctx context.Context, d *Device, wg *sync.WaitGroup) *worker {
	return &worker{
		ctx: ctx,
		device: d,
		globalWg: wg,
		localWg: &sync.WaitGroup{},
	}
}

func(w *worker) Run() {
	defer w.globalWg.Done()

	// need to calculate number of goroutines to wait for...
	w.localWg.Add(4)

	res, err := w.runCommands(w.ctx)
	if err != nil {
		return
	}
	dataChan, errChan := w.processOutput(res)

	go func() {
		defer w.localWg.Done()
		for e := range errChan {
			WarnLogger.Printf("Got error, device: %q, cmd:%q, error: %q", e.device, e.cmd, e.msg)
		}
	}()
	go w.storeOutput(dataChan)
	w.localWg.Wait()
}

// this func connects to device and issue cli commands
func(w *worker) runCommands(ctx context.Context) (*netrasp.ConfigResult, error) {
	InfoLogger.Printf("Connecting to device %s...\n", w.device.Hostname)
	defer w.localWg.Done()

	//check whether it's recursive call or not
	var legacyDevice bool
	switch ctx.Value("legacyDevice") {
	case nil:
		legacyDevice = false
	default :
		legacyDevice = true
	}
	
	device, err := netrasp.New(w.device.Hostname,
		netrasp.WithUsernamePassword(w.device.Login, w.device.Password),
		netrasp.WithDriver(w.device.OsType), netrasp.WithInsecureIgnoreHostKey(),
		netrasp.WithDialTimeout(time.Duration(appConfig.Client.SSHTimeout)*time.Second),
	)
	if legacyDevice {
		device, err = netrasp.New(w.device.Hostname,
			netrasp.WithUsernamePassword(w.device.Login, w.device.Password),
			netrasp.WithDriver(w.device.OsType), netrasp.WithInsecureIgnoreHostKey(),
			netrasp.WithDialTimeout(time.Duration(appConfig.Client.SSHTimeout)*time.Second),
			netrasp.WithSSHKeyExchange(appConfig.Client.LegacyKeyExchange),
			netrasp.WithSSHCipher(appConfig.Client.LegacyAlgorithm),
		)
	} 
	if err != nil {
		ErrorLogger.Printf("unable to initialize device: %v\n", err)
		w.device.State = Unknown
		return nil, err
	}

	err = device.Dial(ctx)
	if err != nil {
		w.device.State = Unreachable
		switch {
		// recursive call to "runCommands" in case of ssh ciphers mismatch
		case strings.Contains(err.Error(), "no common algorithm") && !legacyDevice:
			WarnLogger.Printf("Need to lower SSH ciphers for the device %s, retrying...", w.device.Hostname)
			// put exit criteria to ctx
			ctx := context.WithValue(ctx, "legacyDevice", true)
			w.localWg.Add(1)
			return w.runCommands(ctx)
		// case for the same error even after recursive call
		case strings.Contains(err.Error(), "no common algorithm"):
			WarnLogger.Printf("Need to change legacy SSH ciphers in config.yml!")
			WarnLogger.Printf("unable to connect to device %s, err: %v", w.device.Hostname, err)
			return nil, err
		case strings.Contains(err.Error(), "unable to authenticate"):
			w.device.State = SshAuthFailure
			WarnLogger.Printf("unable to authenticate to device %s, error:%v", w.device.Hostname, err)
			return nil, err
		default:
			WarnLogger.Printf("unable to connect to device %s, err: %v", w.device.Hostname, err)
			return nil, err
		}
	}
	defer device.Close(ctx)
	InfoLogger.Printf("Connected to device %s successfully\n", w.device.Hostname)

	// switch between config/show commands
	InfoLogger.Printf("Running commands for device %q...\n", w.device.Hostname)
	if w.device.Configure {
		res, err := device.Configure(ctx, cmdCache[w.device.CmdFile].Commands)
		if err != nil {
			ErrorLogger.Printf("Unable to configure device %s: %v", w.device.Hostname, err)
			w.device.State = PermissionProblem
			return nil, err
		}
		InfoLogger.Printf("Sent commands to device %q successfully\n", w.device.Hostname)
		return &res, nil

		// if d.SaveConfig {
		// 	_, err := device.Run(ctx, d.GetSaveCommand())
		// 	if err != nil {
		// 		d.State = SaveFailed
		// 		ErrorLogger.Printf("Unable to save config for device %s: %v", d.Hostname, err)
		// 	} else if err == nil {
		// 		InfoLogger.Printf("Saved config for device %q successfully\n", d.Hostname)
		// 	}
		// }

	} else {
		// need to construct the same data type as device.Configure method output uses
		// in order to use the same "storeDeviceOutput" processing function further
		var result netrasp.ConfigResult
		for _, cmd := range cmdCache[w.device.CmdFile].Commands {
			res, err := device.Run(ctx, cmd)
			if err != nil {
				ErrorLogger.Printf("unable to run command %s\n", cmd)
				//TODO: find out in which cases this err may show up
				w.device.State = Unknown
				continue
			}
			result.ConfigCommands = append(result.ConfigCommands, netrasp.ConfigCommand{Command: cmd, Output: res})
		}
		InfoLogger.Printf("Sent commands to device %q successfully\n", w.device.Hostname)
		return &result, nil

		// if d.SaveConfig {
		// 	_, err := device.Run(ctx, d.GetSaveCommand())
		// 	if err != nil {
		// 		d.State = SaveFailed
		// 		ErrorLogger.Printf("Unable to save config for device %s: %v", d.Hostname, err)
		// 	} else if err == nil {
		// 		InfoLogger.Printf("Saved config for device %q successfully\n", d.Hostname)
		// 	}
		// }
	}
}


func(w *worker) processOutput(inData *netrasp.ConfigResult) (chan string, chan devError) {
	errChan := make(chan devError, 10)
	dataChan := make(chan string)

	go func() {
		defer w.localWg.Done()
		defer close(errChan)
		defer close(dataChan)

		for _, r := range inData.ConfigCommands {
			var commandError string
			var errFound bool
	
			if w.device.Configure {
				commandError, errFound = detectCliErrors(r.Output)
			} else {
				// need to trim output to up to first 3 lines, because error message contained there
				linesCount := len(strings.Split(r.Output, "\n"))
				var linesToSlice = 3
				if linesCount < 3 {
					// some errors output are just one line
					linesToSlice = 1
				}
				partialOutput := strings.Join(strings.Split(r.Output, "\n")[:linesToSlice], "\n")
				commandError, errFound = detectCliErrors(partialOutput)
			}
	
			if errFound {
				w.device.State = CmdPartiallyAccepted
				errChan <- devError{device: w.device.Hostname, cmd: r.Command, msg: commandError}
			} else {
				// in order no to overwrite previous "Partially accepted" state
				if w.device.State != CmdPartiallyAccepted {
					w.device.State = Ok
				}
			}
	
			var row string
			if w.device.Configure {
				row = fmt.Sprintf("device: %q, command: %q, accepted: %t, error: %q\n",
				w.device.Hostname, r.Command, !errFound, commandError)
			} else {
				row = fmt.Sprintf("device: %q, command: %q, accepted: %t, error: %q output:\n%s\n==========================================\n",
				w.device.Hostname, r.Command, !errFound, commandError, r.Output)
				if errFound {
					row = fmt.Sprintf("device: %q, command: %q, accepted: %t, error: %q\n==========================================\n",
					w.device.Hostname, r.Command, !errFound, commandError)
				}
			}
			dataChan <- row
		}
	}()
	return dataChan, errChan	
}


// this func stores commands output to file, config and non-config commands have different output formatting
func(w *worker) storeOutput(inData chan string) {
	defer w.localWg.Done()
	InfoLogger.Printf("Storing device %q data to file...", w.device.Hostname)

	f, err := os.OpenFile(filepath.Join(appConfig.Data.OutputFolder, w.device.Hostname+"_commandStatus.txt"), os.O_APPEND|os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		ErrorLogger.Printf("Unable to open output file for device %s because of: %s\nDevice output will not be stored!", w.device.Hostname, err)
		return
	}
	defer f.Close()
	writer := bufio.NewWriter(f)
	writer.WriteString(fmt.Sprintf("======================== %q =======================\n", time.Now().Format(time.RFC822)))

	for row := range inData {
		writer.WriteString(row)
	}

	err = writer.Flush()
	if err != nil {
		ErrorLogger.Printf("Unable to write output for device %q to file %q\n because of:%q", w.device.Hostname, f.Name(), err)
	} else {
		InfoLogger.Printf("Stored device %q data to file successfully\n", w.device.Hostname)
	}
}
