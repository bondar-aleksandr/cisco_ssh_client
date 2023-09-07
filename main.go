package main

import (
	"context"
	"errors"
	"github.com/bondar-aleksandr/netrasp/pkg/netrasp"
	"github.com/gocarina/gocsv"
	"log"
	"os"
	"path/filepath"
	"time"
)

// describes entry in csv device file
type Device struct {
	Hostname  string `csv:"hostname"`
	Login     string `csv:"login"`
	Password  string `csv:"password"`
	OsType    string `csv:"osType"`
	Configure bool   `csv:"configure"`
	CmdFile   string `csv:"CmdFile"`
}

// type for app-level config
type config struct {
	Client struct {
		SSHTimeout int64 `yaml:"ssh_timeout"`
	}
	Data struct {
		InputFolder  string `yaml:"input_folder"`
		DevicesData  string `yaml:"devices_data"`
		OutputFolder string `yaml:"output_folder"`
	}
}

// type used for storing all commands from single command file
type Commands struct {
	Commands []string
}

func (c *Commands) Add(cmd string) {
	c.Commands = append(c.Commands, cmd)
}

// stores mapping between command file and its content, only unique entries present
var cmdCache = make(map[string]*Commands)
var configPath = "./config/config.yml"
var appConfig config
var (
	InfoLogger  *log.Logger = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLogger *log.Logger = log.New(os.Stdout, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
)

func main() {
	InfoLogger.Println("Starting...")

	readConfig(&appConfig)

	//Parse CSV with devices info to memory
	InfoLogger.Println("Decoding devices data...")

	deviceFile, err := os.Open(filepath.Join(appConfig.Data.InputFolder, appConfig.Data.DevicesData))
	if err != nil {
		ErrorLogger.Fatal(err)
	}
	defer deviceFile.Close()

	var devices []*Device

	if err := gocsv.UnmarshalFile(deviceFile, &devices); err != nil {
		ErrorLogger.Fatalf("Cannot unmarshal CSV from file because of: %s", err)
	}
	InfoLogger.Println("Decoding devices data done")

	//build command files cache
	buildCmdCache(devices)

	// looping over devices
	for _, d := range devices {

		InfoLogger.Printf("Connecting to device %s...\n", d.Hostname)
		device, err := netrasp.New(d.Hostname,
			netrasp.WithUsernamePassword(d.Login, d.Password),
			netrasp.WithDriver(d.OsType), netrasp.WithInsecureIgnoreHostKey(),
			netrasp.WithDialTimeout(time.Duration(appConfig.Client.SSHTimeout)*time.Second),
		)
		if err != nil {
			ErrorLogger.Fatalf("unable to initialize device: %v", err)
		}

		err = device.Dial(context.Background())
		if err != nil {
			ErrorLogger.Fatalf("unable to connect: %v", err)
		}
		defer device.Close(context.Background())
		InfoLogger.Printf("Connected to device %s successfully\n", d.Hostname)

		// switch between config/show commands
		InfoLogger.Printf("Configuring device %q...\n", d.Hostname)
		if d.Configure {
			res, err := device.Configure(context.Background(), cmdCache[d.CmdFile].Commands)

			if errors.Is(err, netrasp.IncorrectConfigCommandErr) {
				ErrorLogger.Printf("Device: %s, one of config commands failed, further commands skipped!\n", d.Hostname)
			} else if err != nil {
				ErrorLogger.Fatalf("unable to configure device %s: %v", d.Hostname, err)
			} else if err == nil {
				InfoLogger.Printf("Configured device %q successfully\n", d.Hostname)
			}
			//config result analysis
			InfoLogger.Printf("Storing device %q data to file...", d.Hostname)
			err = storeConfigResult(&res, d.Hostname)
			if err != nil {
				ErrorLogger.Printf("Storing device %q data to file failed because of err: %q", d.Hostname, err)
			} else {
				InfoLogger.Printf("Stored device %q data to file successfully\n", d.Hostname)
			}

		} else {
			var result netrasp.ConfigResult
			for _, cmd := range cmdCache[d.CmdFile].Commands {
				res, err := device.Run(context.Background(), cmd)
				if err != nil {
					ErrorLogger.Printf("unable to run command %s\n", cmd)
					continue
				}
				result.ConfigCommands = append(result.ConfigCommands, netrasp.ConfigCommand{Command: cmd, Output: res})
			}
			err = storeConfigResult(&result, d.Hostname)
		}
	}
}
