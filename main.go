package main

import (
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
	CmdFile   string `csv:"CmdFile"`
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

// type describes general connectivity errors
type connError struct {
	device string
	error  error
}

// to describe command run status
const (
	Ok                   = "Success"
	Unreachable          = "Unreachable"
	Unknown              = "Unknown"
	SshAuthFailure       = "SSH authentication failure"
	PermissionProblem    = "Permission problem"
	CmdPartiallyAccepted = "Commands accepted with errors"
)

// stores mapping between command file and its content, only unique entries present
var cmdCache = make(map[string]*Commands)
var configPath = "./config/config.yml"
var appConfig config
var (
	InfoLogger  *log.Logger = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLogger *log.Logger = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
)

func main() {
	start := time.Now()
	InfoLogger.Println("Starting...")

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		s := <-quit
		ErrorLogger.Printf("Caught signal: %q", s.String())
		os.Exit(0)
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

	// initialize wg to sync goroutines
	var cmdWg sync.WaitGroup
	cmdWg.Add(len(devices))

	//channel for cli errors notification
	cliErrChan := make(chan cliError, 100)
	//channel for general errors notification
	connErrChan := make(chan connError, len(devices))

	// looping over devices
	for _, d := range devices {
		go runCommands(d, &cmdWg, cliErrChan, connErrChan)
	}
	cmdWg.Wait()
	close(cliErrChan)
	close(connErrChan)

	// initialize wg to sync error reading
	var errWg sync.WaitGroup
	errWg.Add(2)

	// read connectivity errors from connErrChan
	go func() {
		defer errWg.Done()
		for e := range connErrChan {
			ErrorLogger.Printf("Got general failure, device: %q, error: %q", e.device, e.error)
		}
	}()

	// read cli errors from cliErrChan
	go func() {
		defer errWg.Done()
		for e := range cliErrChan {
			ErrorLogger.Printf("Got command run failure, device: %q, command: %q, error: %q", e.device, e.cmd, e.error)
		}
	}()

	errWg.Wait()

	//write summary output
	resultsFile, err := os.Create(filepath.Join(appConfig.Data.OutputFolder, appConfig.Data.ResultsData))
	if err != nil {
		ErrorLogger.Printf("Unable to create summary output file because of: %q", err)
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
	resultsFile.WriteString(tableString.String())
	fmt.Println(tableString.String())

	InfoLogger.Printf("Finished! Time taken: %s\n", time.Since(start))
}
