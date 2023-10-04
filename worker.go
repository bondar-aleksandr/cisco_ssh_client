package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/bondar-aleksandr/netrasp/pkg/netrasp"
)

// this func connects to device and issue cli commands
func runCommands(ctx context.Context, d *Device, wg *sync.WaitGroup, errChan chan<- devError) {
	InfoLogger.Printf("Connecting to device %s...\n", d.Hostname)
	defer wg.Done()

	//check whether it's recursive call or not
	var legacyDevice bool
	switch ctx.Value("legacyDevice") {
	case nil:
		legacyDevice = false
	default :
		legacyDevice = true
	}
	
	device, err := netrasp.New(d.Hostname,
		netrasp.WithUsernamePassword(d.Login, d.Password),
		netrasp.WithDriver(d.OsType), netrasp.WithInsecureIgnoreHostKey(),
		netrasp.WithDialTimeout(time.Duration(appConfig.Client.SSHTimeout)*time.Second),
	)
	if legacyDevice {
		device, err = netrasp.New(d.Hostname,
			netrasp.WithUsernamePassword(d.Login, d.Password),
			netrasp.WithDriver(d.OsType), netrasp.WithInsecureIgnoreHostKey(),
			netrasp.WithDialTimeout(time.Duration(appConfig.Client.SSHTimeout)*time.Second),
			netrasp.WithSSHKeyExchange(appConfig.Client.LegacyKeyExchange),
			netrasp.WithSSHCipher(appConfig.Client.LegacyAlgorithm),
		)
	} 
	if err != nil {
		ErrorLogger.Printf("unable to initialize device: %v\n", err)
		d.State = Unknown
		return
	}

	err = device.Dial(ctx)
	if err != nil {
		d.State = Unreachable
		switch {
		// recursive call to "runCommands" in case of ssh ciphers mismatch
		case strings.Contains(err.Error(), "no common algorithm for key exchange") && !legacyDevice:
			WarnLogger.Printf("Need to lower SSH ciphers for the device %s, retrying...", d.Hostname)
			// put exit criteria to ctx
			ctx := context.WithValue(ctx, "legacyDevice", true)
			wg.Add(1)
			runCommands(ctx, d, wg, errChan)
			return
		// case for the same error even after recursive call
		case strings.Contains(err.Error(), "no common algorithm for key exchange"):
			WarnLogger.Printf("Need to change legacy SSH ciphers in config.yml!")
			WarnLogger.Printf("unable to connect to device %s, err: %v", d.Hostname, err)
			return
		case strings.Contains(err.Error(), "unable to authenticate"):
			d.State = SshAuthFailure
			WarnLogger.Printf("unable to authenticate to device %s, error:%v", d.Hostname, err)
			return
		default:
			WarnLogger.Printf("unable to connect to device %s, err: %v", d.Hostname, err)
			return
		}
	}

	defer device.Close(ctx)
	InfoLogger.Printf("Connected to device %s successfully\n", d.Hostname)

	// switch between config/show commands
	InfoLogger.Printf("Running commands for device %q...\n", d.Hostname)
	if d.Configure {
		res, err := device.Configure(ctx, cmdCache[d.CmdFile].Commands)
		if err != nil {
			ErrorLogger.Printf("unable to configure device %s: %v", d.Hostname, err)
			d.State = PermissionProblem
		} else if err == nil {
			d.State = Ok
			InfoLogger.Printf("Configured device %q successfully\n", d.Hostname)
		}
		//output analysis
		storeDeviceOutput(&res, d, errChan)

	} else {
		// need to construct the same data type as device.Configure method output uses
		// in order to use the same "storeDeviceOutput" processing function further
		var result netrasp.ConfigResult
		for _, cmd := range cmdCache[d.CmdFile].Commands {
			res, err := device.Run(ctx, cmd)
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
		storeDeviceOutput(&result, d, errChan)
	}
}
