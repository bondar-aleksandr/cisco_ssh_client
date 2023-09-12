package main

import (
	"context"
	"errors"
	"github.com/bondar-aleksandr/netrasp/pkg/netrasp"
	"strings"
	"sync"
	"time"
)

// this func connects to device and issue cli commands
func runCommands(d *Device, wg *sync.WaitGroup, cliErrChan chan<- cliError) {
	InfoLogger.Printf("Connecting to device %s...\n", d.Hostname)
	defer wg.Done()
	device, err := netrasp.New(d.Hostname,
		netrasp.WithUsernamePassword(d.Login, d.Password),
		netrasp.WithDriver(d.OsType), netrasp.WithInsecureIgnoreHostKey(),
		netrasp.WithDialTimeout(time.Duration(appConfig.Client.SSHTimeout)*time.Second),
	)
	if err != nil {
		ErrorLogger.Printf("unable to initialize device: %v\n", err)
		d.State = Unknown
		return
	}

	err = device.Dial(context.Background())
	if err != nil {
		d.State = Unreachable
		if strings.Contains(err.Error(), "unable to authenticate") {
			d.State = SshAuthFailure
		}
		ErrorLogger.Println(err)
		return
	}
	defer device.Close(context.Background())
	InfoLogger.Printf("Connected to device %s successfully\n", d.Hostname)

	// switch between config/show commands
	InfoLogger.Printf("Running commands for device %q...\n", d.Hostname)
	if d.Configure {
		res, err := device.Configure(context.Background(), cmdCache[d.CmdFile].Commands)

		if errors.Is(err, netrasp.IncorrectConfigCommandErr) {
			ErrorLogger.Printf("Device: %s, one of config commands failed, further commands skipped!\n", d.Hostname)
			d.State = CmdPartiallyAccepted
		} else if err != nil {
			ErrorLogger.Printf("unable to configure device %s: %v", d.Hostname, err)
			d.State = PermissionProblem
		} else if err == nil {
			d.State = Ok
			InfoLogger.Printf("Configured device %q successfully\n", d.Hostname)
		}
		//output analysis
		InfoLogger.Printf("Storing device %q data to file...", d.Hostname)
		err = storeDeviceOutput(&res, d, cliErrChan)
		if err != nil {
			ErrorLogger.Printf("Storing device %q data to file failed because of err: %q", d.Hostname, err)
		} else {
			InfoLogger.Printf("Stored device %q data to file successfully\n", d.Hostname)
		}

	} else {
		// need to construct the same data type as device.Configure method output uses
		// in order to use the same "storeDeviceOutput" processing function further
		var result netrasp.ConfigResult
		for _, cmd := range cmdCache[d.CmdFile].Commands {
			res, err := device.Run(context.Background(), cmd)
			if err != nil {
				ErrorLogger.Printf("unable to run command %s\n", cmd)
				//TODO: find out in which cases this err may show up
				d.State = Unknown
				continue
			}
			result.ConfigCommands = append(result.ConfigCommands, netrasp.ConfigCommand{Command: cmd, Output: res})
		}
		d.State = Ok
		InfoLogger.Printf("Run commands for %q successfully\n", d.Hostname)
		//output analysis
		InfoLogger.Printf("Storing device %q data to file...", d.Hostname)
		err = storeDeviceOutput(&result, d, cliErrChan)
		if err != nil {
			ErrorLogger.Printf("Storing device %q data to file failed because of err: %q", d.Hostname, err)
		} else {
			InfoLogger.Printf("Stored device %q data to file successfully\n", d.Hostname)
		}
	}
}
