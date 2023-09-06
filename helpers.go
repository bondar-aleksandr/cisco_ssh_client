package main

import (
	"bufio"
	"fmt"
	"github.com/bondar-aleksandr/netrasp/pkg/netrasp"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"time"
)

// func receives list of Devices, walk through it, finds unique filenames, and populates
// cmdCache variable with mapping filename:Commands
func BuildCmdCache(entries []*Device) {
	for _, entry := range entries {
		commandsFile, err := os.Open(filepath.Join(appConfig.Data.InputFolder, entry.CmdFile))
		if err != nil {
			ErrorLogger.Fatalf("Unable to open commands file: %s", entry.CmdFile)
		}
		defer commandsFile.Close()

		// check whether info about entry.CmdFile is already in cmdCache map
		if _, ok := cmdCache[entry.CmdFile]; ok {
			continue
		}
		// commands parsing
		commands := Commands{}
		scanner := bufio.NewScanner(commandsFile)
		for scanner.Scan() {
			cmd := scanner.Text()
			commands.Add(cmd)
		}
		// add data to cache
		cmdCache[entry.CmdFile] = &commands
	}
}

// this func Unmarshals config.yml content to config variable
func readConfig(cfg *config) {

	f, err := os.Open(configPath)
	if err != nil {
		ErrorLogger.Fatalf("Cannot read app config file because of: %s", err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(cfg)
	if err != nil {
		ErrorLogger.Fatalf("Cannot parse app config file because of: %s", err)
	}
}

func storeConfigResult(res *netrasp.ConfigResult, hostname string) error {

	f, err := os.OpenFile(filepath.Join(appConfig.Data.OutputFolder, hostname+"_configStatus.txt"), os.O_APPEND|os.O_CREATE, 666)
	if err != nil {
		ErrorLogger.Printf("Unable to open output file for device %s because of: %s", hostname, err)
		return err
	}
	defer f.Close()
	writer := bufio.NewWriter(f)

	for _, r := range res.ConfigCommands {
		commandStatus := true
		if r.Output != "" {
			commandStatus = false
		}
		//TODO: change time format in output
		//TODO: strip command output from spaces
		row := fmt.Sprintf("time: %s, device: %q, command: %q accepted: %t, error: %s\n", time.Now(), hostname, r.Command, commandStatus, r.Output)
		//InfoLogger.Printf("command: %q success: %t, message: %s", r.Command, commandStatus, r.Output)
		writer.WriteString(row)
	}
	err = writer.Flush()
	if err != nil {
		ErrorLogger.Printf("Unable to write output for device %q to file %q\n", hostname, f.Name())
		return err
	}
	return nil
}
