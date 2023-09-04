package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/bondar-aleksandr/netrasp/pkg/netrasp"
	"github.com/gocarina/gocsv"
	"log"
	"os"
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

// type used for storing all commands from single command file
type Commands struct {
	Commands []string
}

func (c *Commands) Add(cmd string) {
	c.Commands = append(c.Commands, cmd)
}

// stores mapping between command file and its content, only unique entries present
var cmdCache = make(map[string]*Commands)

var (
	InfoLogger  *log.Logger = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLogger *log.Logger = log.New(os.Stdout, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
)

func main() {

	InfoLogger.Println("Starting...")

	//Parse CSV with devices info to memory
	deviceFile, err := os.Open("devices.csv")
	if err != nil {
		ErrorLogger.Fatal(err)
	}
	defer deviceFile.Close()

	devices := []*Device{}

	if err := gocsv.UnmarshalFile(deviceFile, &devices); err != nil {
		ErrorLogger.Fatalf("Cannot unmarshal CSV from file because of: %s", err)
	}

	//build command files cache
	BuildCmdCache(devices)
	InfoLogger.Println("Successfully build cmd cache...")

	for _, d := range devices {

		device, err := netrasp.New(d.Hostname,
			netrasp.WithUsernamePassword(d.Login, d.Password),
			netrasp.WithDriver(d.OsType), netrasp.WithInsecureIgnoreHostKey(),
		)
		if err != nil {
			ErrorLogger.Fatalf("unable to initialize device: %v", err)
		}

		err = device.Dial(context.Background())
		if err != nil {
			ErrorLogger.Fatalf("unable to connect: %v", err)
		}
		defer device.Close(context.Background())

		// switch between config/show commands
		if d.Configure {
			res, err := device.Configure(context.Background(), cmdCache[d.CmdFile].Commands)

			if errors.Is(err, netrasp.IncorrectConfigCommandErr) {
				InfoLogger.Println("one of config commands failed, further commands skipped!")
			} else if err != nil {
				ErrorLogger.Fatalf("unable to configure device: %v", err)
			}
			//config result analysis
			for _, r := range res.ConfigCommands {
				commandStatus := true
				if r.Output != "" {
					commandStatus = false
				}
				InfoLogger.Printf("command: %q success: %t, message: %s", r.Command, commandStatus, r.Output)
			}
		} else {
			for _, cmd := range cmdCache[d.CmdFile].Commands {
				res, err := device.Run(context.Background(), cmd)
				if err != nil {
					ErrorLogger.Printf("unable to run command %s\n", cmd)
					continue
				}
				fmt.Println(res)
			}
		}
	}
}
