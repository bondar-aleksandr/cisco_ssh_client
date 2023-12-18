package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"github.com/bondar-aleksandr/cisco_ssh_client/internal/device"
	"github.com/bondar-aleksandr/cisco_ssh_client/internal/app"
	"github.com/bondar-aleksandr/cisco_ssh_client/internal/worker"
	"github.com/gocarina/gocsv"
	"github.com/olekukonko/tablewriter"
)

func main() {
	start := time.Now()
	app, err := app.NewApp("./config/config.yml")
	if err != nil {
		os.Exit(1)
	}

	//graceful shutdown setup
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		s := <-quit
		app.Logger.Errorf("Caught signal: %q, exiting...", s.String())
		cancel()
	}()

	//Parse CSV with devices info to memory
	app.Logger.Info("Decoding devices data...")
	deviceFile, err := os.Open(filepath.Join(app.Config.Data.InputFolder, app.Config.Data.DevicesData))
	if err != nil {
		app.Logger.Error(err)
		os.Exit(1)
	}
	defer deviceFile.Close()

	var devices []*device.Device

	if err := gocsv.UnmarshalFile(deviceFile, &devices); err != nil {
		app.Logger.Errorf("Cannot unmarshal CSV from file because of: %s", err)
		os.Exit(1)
	}
	app.Logger.Info("Decoding devices data done")

	//build command files cache
	app.BuildCmdCache(devices)

	// initialize cmdWg to sync worker goroutines
	var cmdWg sync.WaitGroup
	cmdWg.Add(len(devices))

	for _, d := range devices {
		w := worker.NewWorker(ctx, d, &cmdWg, app)
		go w.Run()
	}
	cmdWg.Wait()

	//write summary output
	app.Logger.Info("Writing app summary output...")
	resultsFile, err := os.OpenFile(filepath.Join(app.Config.Data.OutputFolder, app.Config.Data.ResultsData), os.O_CREATE|os.O_APPEND|os.O_RDWR, os.ModePerm)
	if err != nil {
		app.Logger.Errorf("Unable to create app summary output file because of: %q", err)
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
		app.Logger.Errorf("Unable to write app summary because of: %q", err)
	}
	app.Logger.Info("Writing app summary output done")
	fmt.Println(tableString.String())

	app.Logger.Infof("Finished! Time taken: %s", time.Since(start))
}