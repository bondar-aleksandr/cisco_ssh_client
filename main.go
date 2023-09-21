package main

import (
	"context"
	"fmt"
	"github.com/gocarina/gocsv"
	"github.com/olekukonko/tablewriter"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// describes entry in csv device file
type Device struct {
	Hostname  string `csv:"hostname"`
	Login     string `csv:"login"`
	Password  string `csv:"password"`
	OsType    string `csv:"osType"`
	Configure bool   `csv:"configure"`
	CmdFile   string `csv:"cmdFile"`
	State     string
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
		ResultsData  string `yaml:"results_data"`
	}
}

// type used for storing all commands from single command file
type Commands struct {
	Commands []string
}

func (c *Commands) Add(cmd string) {
	c.Commands = append(c.Commands, cmd)
}

// type describes cli error
type cliError struct {
	device string
	cmd    string
	error  string
}

// to describe command run status
const (
	Ok                   = "Success"
	Unreachable          = "Unreachable"
	Unknown              = "Unknown"
	SshAuthFailure       = "SSH authentication failure"
	PermissionProblem    = "Permission problem/Canceled"
	CmdPartiallyAccepted = "Commands accepted with errors"
)

// stores mapping between command file and its content, only unique entries present
var cmdCache = make(map[string]*Commands)
var configPath = "./config/config.yml"
var appConfig config
var (
	InfoLogger  *log.Logger = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	WarnLogger *log.Logger = log.New(os.Stdout, "WARNING: ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLogger *log.Logger = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
)

func main() {
	start := time.Now()
	InfoLogger.Println("Starting...")

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		s := <-quit
		ErrorLogger.Printf("Caught signal: %q, exiting...", s.String())
		cancel()
	}()

	// read app config
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

	// initialize cmdWg to sync worker goroutines
	var cmdWg sync.WaitGroup
	cmdWg.Add(len(devices))

	//channel for cli errors notification
	cliErrChan := make(chan cliError, 100)

	// looping over devices
	for _, d := range devices {
		go runCommands(d, &cmdWg, cliErrChan, ctx)
	}

	// create wg to wait till cliErrChan is drained
	var errWg sync.WaitGroup
	errWg.Add(1)

	//read cli errors from cliErrChan in background
	go func() {
		defer errWg.Done()
		for e := range cliErrChan {
			WarnLogger.Printf("Got command run failure, device: %q, command: %q, error: %q", e.device, e.cmd, e.error)
		}
	}()
	// wait till all workers are done
	cmdWg.Wait()
	//close channel when all sending to it goroutines exits
	close(cliErrChan)

	// wait till cliErrChan is drained
	errWg.Wait()

	//write summary output
	InfoLogger.Println("Writing app summary output...")
	resultsFile, err := os.Create(filepath.Join(appConfig.Data.OutputFolder, appConfig.Data.ResultsData))
	if err != nil {
		ErrorLogger.Printf("Unable to create app summary output file because of: %q", err)
	}
	defer resultsFile.Close()

	tableString := &strings.Builder{}
	table := tablewriter.NewWriter(tableString)
	table.SetHeader([]string{"Device", "OS type", "configure", "Command Run Status"})

	for _, d := range devices {
		table.Append([]string{d.Hostname, d.OsType, strconv.FormatBool(d.Configure), d.State})
	}
	table.SetFooter([]string{"", "", "", time.Now().Format(time.RFC822)})
	table.Render()
	_, err = resultsFile.WriteString(tableString.String())
	if err != nil {
		ErrorLogger.Printf("Unable to write app summary because of: %q", err)
	}
	InfoLogger.Println("Writing app summary output done")
	fmt.Println(tableString.String())

	InfoLogger.Printf("Finished! Time taken: %s\n", time.Since(start))
}
