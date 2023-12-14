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
	"github.com/bondar-aleksandr/cisco_ssh_client/logger"
	"log/slog"
)

// type describes worker, which is responsible for gathering data from device, processing, and storing the data
type worker struct {
	device *Device
	globalWg *sync.WaitGroup
	localWg *sync.WaitGroup
	ctx context.Context
	logger *slog.Logger
}

// constructor for worker
func NewWorker(ctx context.Context, d *Device, wg *sync.WaitGroup) *worker {
	return &worker{
		ctx: ctx,
		device: d,
		globalWg: wg,
		localWg: &sync.WaitGroup{},
		logger: logger.L.With("device", d.Hostname),
	}
}

// main process for worker
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
			w.logger.Warn("Got error", "command", e.cmd, "error", e.msg)
		}
	}()
	go w.storeOutput(dataChan)
	w.localWg.Wait()
}

// this func connects to device and issue cli commands
func(w *worker) runCommands(ctx context.Context) (*netrasp.ConfigResult, error) {
	w.logger.Info("Connecting to device...")
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
		w.logger.Error("unable to initialize device", "error", err.Error())
		w.device.State = Unknown
		return nil, err
	}

	err = device.Dial(ctx)
	if err != nil {
		w.device.State = Unreachable
		switch {
		// recursive call to "runCommands" in case of ssh ciphers mismatch
		case strings.Contains(err.Error(), "no common algorithm") && !legacyDevice:
			w.logger.Warn("Need to lower SSH ciphers for the device, retrying...")
			// put exit criteria to ctx
			ctx := context.WithValue(ctx, "legacyDevice", true)
			w.localWg.Add(1)
			return w.runCommands(ctx)
		// case for the same error even after recursive call
		case strings.Contains(err.Error(), "no common algorithm"):
			logger.L.Warn("Need to change legacy SSH ciphers in config.yml!")
			w.logger.Warn("unable to connect to device", "error", err.Error())
			return nil, err
		case strings.Contains(err.Error(), "unable to authenticate"):
			w.device.State = SshAuthFailure
			w.logger.Warn("unable to authenticate to device", "error", err.Error())
			return nil, err
		default:
			w.logger.Warn("unable to connect to device", "error", err.Error())
			return nil, err
		}
	}
	defer device.Close(ctx)
	w.logger.Info("Connected to device successfully")

	// switch between config/show commands
	w.logger.Info("Running commands for device...")
	if w.device.Configure {
		res, err := device.Configure(ctx, cmdCache[w.device.CmdFile].Commands)
		if err != nil {
			w.logger.Error("Unable to configure device", "error", err.Error())
			w.device.State = PermissionProblem
			return nil, err
		}
		w.logger.Info("Sent commands to device successfully")
		return &res, nil

	} else {
		// need to construct the same data type as device.Configure method output uses
		// in order to use the same "storeDeviceOutput" processing function further
		var result netrasp.ConfigResult
		for _, cmd := range cmdCache[w.device.CmdFile].Commands {
			res, err := device.Run(ctx, cmd)
			if err != nil {
				w.logger.Error("unable to run command", "command", cmd)
				//TODO: find out in which cases this err may show up
				w.device.State = Unknown
				continue
			}
			result.ConfigCommands = append(result.ConfigCommands, netrasp.ConfigCommand{Command: cmd, Output: res})
		}
		w.logger.Info("Sent commands to device successfully")
		return &result, nil
	}
}

// processes output from CLI commands, returns channel with formatted (for persistance) data and channel with errors
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
	w.logger.Info("Storing device data to file...")

	f, err := os.OpenFile(filepath.Join(appConfig.Data.OutputFolder, w.device.Hostname+"_commandStatus.txt"), os.O_APPEND|os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		w.logger.Error("Unable to open output file for device. Device output will not be stored!", "error", err)
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
		w.logger.Error("Unable to write output for device to file", "file", f.Name(), "error", err)
	} else {
		w.logger.Info("Stored device data to file successfully")
	}
}
