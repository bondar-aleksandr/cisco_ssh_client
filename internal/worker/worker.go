package worker

import (
	"context"
	"strings"
	"sync"
	"time"
	"github.com/bondar-aleksandr/netrasp/pkg/netrasp"
	"github.com/bondar-aleksandr/cisco_ssh_client/internal/device"
	"github.com/bondar-aleksandr/cisco_ssh_client/internal/app"
	"fmt"
	"os"
	"bufio"
	"path/filepath"
)

// type describes worker, which is responsible for gathering data from device, processing, and storing the data
type worker struct {
	device *device.Device
	globalWg *sync.WaitGroup
	localWg *sync.WaitGroup
	ctx context.Context
	app *app.App		//pointer to parent app
}

// constructor for worker
func NewWorker(ctx context.Context, d *device.Device, wg *sync.WaitGroup, a *app.App) *worker {
	return &worker{
		ctx: ctx,
		device: d,
		globalWg: wg,
		localWg: &sync.WaitGroup{},
		app: a,
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
			w.app.Logger.Warnf("Got error, device: %q, cmd:%q, error: %q", e.Device, e.Cmd, e.Msg)
		}
	}()
	go w.storeOutput(dataChan)
	w.localWg.Wait()
}

// this func connects to device and issue cli commands
func(w *worker) runCommands(ctx context.Context) (*netrasp.ConfigResult, error) {
	w.app.Logger.Infof("Connecting to device %s...", w.device.Hostname)
	defer w.localWg.Done()

	//check whether it's recursive call or not
	var legacyDevice bool
	switch ctx.Value("legacyDevice") {
	case nil:
		legacyDevice = false
	default :
		legacyDevice = true
	}
	
	d, err := netrasp.New(w.device.Hostname,
		netrasp.WithUsernamePassword(w.device.Login, w.device.Password),
		netrasp.WithDriver(w.device.OsType), netrasp.WithInsecureIgnoreHostKey(),
		netrasp.WithDialTimeout(time.Duration(w.app.Config.Client.SSHTimeout)*time.Second),
	)
	if legacyDevice {
		d, err = netrasp.New(w.device.Hostname,
			netrasp.WithUsernamePassword(w.device.Login, w.device.Password),
			netrasp.WithDriver(w.device.OsType), netrasp.WithInsecureIgnoreHostKey(),
			netrasp.WithDialTimeout(time.Duration(w.app.Config.Client.SSHTimeout)*time.Second),
			netrasp.WithSSHKeyExchange(w.app.Config.Client.LegacyKeyExchange),
			netrasp.WithSSHCipher(w.app.Config.Client.LegacyAlgorithm),
		)
	} 
	if err != nil {
		w.app.Logger.Errorf("unable to initialize device: %v", err)
		w.device.State = device.Unknown
		return nil, err
	}

	err = d.Dial(ctx)
	if err != nil {
		w.device.State = device.Unreachable
		switch {
		// recursive call to "runCommands" in case of ssh ciphers mismatch
		case strings.Contains(err.Error(), "no common algorithm") && !legacyDevice:
			w.app.Logger.Warnf("Need to lower SSH ciphers for the device %s, retrying...", w.device.Hostname)
			// put exit criteria to ctx
			ctx := context.WithValue(ctx, "legacyDevice", true)
			w.localWg.Add(1)
			return w.runCommands(ctx)
		// case for the same error even after recursive call
		case strings.Contains(err.Error(), "no common algorithm"):
			w.app.Logger.Warnf("Need to change legacy SSH ciphers in config.yml!")
			w.app.Logger.Warnf("unable to connect to device %s, err: %v", w.device.Hostname, err)
			return nil, err
		case strings.Contains(err.Error(), "unable to authenticate"):
			w.device.State = device.SshAuthFailure
			w.app.Logger.Warnf("unable to authenticate to device %s, error:%v", w.device.Hostname, err)
			return nil, err
		default:
			w.app.Logger.Warnf("unable to connect to device %s, err: %v", w.device.Hostname, err)
			return nil, err
		}
	}
	defer d.Close(ctx)
	w.app.Logger.Infof("Connected to device %s successfully", w.device.Hostname)

	// switch between config/show commands
	w.app.Logger.Infof("Running commands for device %q...", w.device.Hostname)
	if w.device.Configure {
		res, err := d.Configure(ctx, w.app.CmdCache[w.device.CmdFile].Commands)
		if err != nil {
			w.app.Logger.Errorf("Unable to configure device %s: %v", w.device.Hostname, err)
			w.device.State = device.PermissionProblem
			return nil, err
		}
		w.app.Logger.Infof("Sent commands to device %q successfully", w.device.Hostname)
		return &res, nil

	} else {
		// need to construct the same data type as device.Configure method output uses
		// in order to use the same "storeDeviceOutput" processing function further
		var result netrasp.ConfigResult
		for _, cmd := range w.app.CmdCache[w.device.CmdFile].Commands {
			res, err := d.Run(ctx, cmd)
			if err != nil {
				w.app.Logger.Errorf("unable to run command %s", cmd)
				//TODO: find out in which cases this err may show up
				w.device.State = device.Unknown
				continue
			}
			result.ConfigCommands = append(result.ConfigCommands, netrasp.ConfigCommand{Command: cmd, Output: res})
		}
		w.app.Logger.Infof("Sent commands to device %q successfully", w.device.Hostname)
		return &result, nil
	}
}

// processes output from CLI commands, returns channel with formatted (for persistance) data and channel with errors
func(w *worker) processOutput(inData *netrasp.ConfigResult) (chan string, chan device.DevError) {
	errChan := make(chan device.DevError, 10)
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
				w.device.State = device.CmdPartiallyAccepted
				errChan <- device.DevError{Device: w.device.Hostname, Cmd: r.Command, Msg: commandError}
			} else {
				// in order no to overwrite previous "Partially accepted" state
				if w.device.State != device.CmdPartiallyAccepted {
					w.device.State = device.Ok
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
	w.app.Logger.Infof("Storing device %q data to file...", w.device.Hostname)

	f, err := os.OpenFile(filepath.Join(w.app.Config.Data.OutputFolder, w.device.Hostname+"_commandStatus.txt"), os.O_APPEND|os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		w.app.Logger.Errorf("Unable to open output file for device %s because of: %s\nDevice output will not be stored!", w.device.Hostname, err)
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
		w.app.Logger.Errorf("Unable to write output for device %q to file %q\n because of:%q", w.device.Hostname, f.Name(), err)
	} else {
		w.app.Logger.Infof("Stored device %q data to file successfully", w.device.Hostname)
	}
}


// this func looks for error in CLI output. Returns
// string with error and bool if error found
func detectCliErrors(input string) (string, bool) {
	rows := strings.Split(input, "\n")
	var cliErr string
	var errFound bool
	for _, r := range rows {
		if strings.HasPrefix(r, "%") || strings.HasPrefix(r, "Command rejected:") {
			cliErr = r
			errFound = true
			break
		}
	}
	return cliErr, errFound
}