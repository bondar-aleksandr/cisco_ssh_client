package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gocarina/gocsv"
	"github.com/olekukonko/tablewriter"

	// "log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	// "log/slog"
	"github.com/bondar-aleksandr/cisco_ssh_client/logger"
)

// to describe command run status
const (
	Ok                   = "Success"
	Unreachable          = "Unreachable"
	Unknown              = "Unknown"
	SshAuthFailure       = "SSH authentication failure"
	PermissionProblem    = "Permission problem/Canceled"
	CmdPartiallyAccepted = "Commands accepted with errors"
	SaveFailed			 = "Commands accepted, save config failed"
)

// stores mapping between command file and its content, only unique entries present
var cmdCache = make(map[string]*Commands)
var configPath = "./config/config.yml"
var appConfig config
// var (
// 	InfoLogger  *log.Logger = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
// 	WarnLogger *log.Logger = log.New(os.Stdout, "WARNING: ", log.Ldate|log.Ltime|log.Lshortfile)
// 	ErrorLogger *log.Logger = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
// )

func main() {
	start := time.Now()
	// InfoLogger.Println("Starting...")
	logger.L.Info("Starting...")

	//graceful shutdown setup
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		s := <-quit
		logger.L.Error("Caught signal, exiting...", "signal", s.String())
		// ErrorLogger.Printf("Caught signal: %q, exiting...", s.String())
		cancel()
	}()

	// read app config
	readConfig(&appConfig)

	//TODO: set level based on config
	logger.ProgramLevel.Set(slog.LevelDebug)

	//create "output" directory
	err := prepareDirectory()
	if err != nil {
		logger.L.Error("Cannot create directory for outputs, exiting...", "error", err.Error())
		os.Exit(1)
		// ErrorLogger.Fatalf("Cannot create directory for outputs because of: %q, exiting...", err)
	}

	//Parse CSV with devices info to memory
	// InfoLogger.Println("Decoding devices data...")
	logger.L.Info("Decoding devices data...")
	deviceFile, err := os.Open(filepath.Join(appConfig.Data.InputFolder, appConfig.Data.DevicesData))
	if err != nil {
		// ErrorLogger.Fatal(err)
		logger.L.Error("Failed to open devices file", "error", err.Error())
		os.Exit(1)
	}
	defer deviceFile.Close()

	var devices []*Device

	if err := gocsv.UnmarshalFile(deviceFile, &devices); err != nil {
		// ErrorLogger.Fatalf("Cannot unmarshal CSV from file because of: %s", err)
		logger.L.Error("Cannot unmarshal CSV from file", "error", err.Error())
		os.Exit(1)
	}
	// InfoLogger.Println("Decoding devices data done")
	logger.L.Info("Decoding devices data done")

	//build command files cache
	buildCmdCache(devices)

	// initialize cmdWg to sync worker goroutines
	var cmdWg sync.WaitGroup
	cmdWg.Add(len(devices))

	for _, d := range devices {
		w := NewWorker(ctx, d, &cmdWg)
		go w.Run()
	}
	cmdWg.Wait()

	//write summary output
	// InfoLogger.Println("Writing app summary output...")
	logger.L.Info("Writing app summary output...")
	resultsFile, err := os.OpenFile(filepath.Join(appConfig.Data.OutputFolder, appConfig.Data.ResultsData), os.O_CREATE|os.O_APPEND|os.O_RDWR, os.ModePerm)
	if err != nil {
		// ErrorLogger.Printf("Unable to create app summary output file because of: %q", err)
		logger.L.Error("Unable to create app summary output file", "error", err.Error())
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
		logger.L.Error("Unable to write app summary", "error", err.Error())
		// ErrorLogger.Printf("Unable to write app summary because of: %q", err)
	}
	// InfoLogger.Println("Writing app summary output done")
	logger.L.Info("Writing app summary output done")
	fmt.Println(tableString.String())

	// InfoLogger.Printf("Finished! Time taken: %s\n", time.Since(start))
	logger.L.Info("Finished!", "time taken", time.Since(start))
}