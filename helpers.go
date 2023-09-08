package main

import (
	"bufio"
	"fmt"
	"github.com/bondar-aleksandr/netrasp/pkg/netrasp"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// func receives list of Devices, walk through it, finds unique filenames, and populates
// cmdCache variable with mapping filename:Commands
func buildCmdCache(entries []*Device) {
	InfoLogger.Println("Building cmd cache...")

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
	InfoLogger.Println("Building cmd cache done")
}

// this func Unmarshals config.yml content to config variable
func readConfig(cfg *config) {
	InfoLogger.Println("Reading config...")

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
	InfoLogger.Println("Reading config done")
	//TODO: print config parameters
}

// this func stores commands output to file, config and non-config commands have different output formatting
func storeDeviceOutput(inData *netrasp.ConfigResult, hostname string, confCommands bool) error {

	f, err := os.OpenFile(filepath.Join(appConfig.Data.OutputFolder, hostname+"_commandStatus.txt"), os.O_APPEND|os.O_CREATE, 666)
	if err != nil {
		ErrorLogger.Printf("Unable to open output file for device %s because of: %s", hostname, err)
		return err
	}
	defer f.Close()
	writer := bufio.NewWriter(f)
	writer.WriteString(fmt.Sprintf("======================== %q =======================\n", time.Now().Format(time.RFC822)))

	for _, r := range inData.ConfigCommands {
		var commandError string
		var errFound bool

		if confCommands {
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

		var row string
		if confCommands {
			row = fmt.Sprintf("device: %q, command: %q, accepted: %t, error: %q\n",
				hostname, r.Command, !errFound, commandError)
		} else {
			if errFound {
				row = fmt.Sprintf("device: %q, command: %q, accepted: %t, error: %q\n==========================================\n",
					hostname, r.Command, !errFound, commandError)
			} else {
				row = fmt.Sprintf("device: %q, command: %q, accepted: %t, error: %q output:\n%s\n==========================================\n",
					hostname, r.Command, !errFound, commandError, r.Output)
			}
		}
		writer.WriteString(row)
	}

	err = writer.Flush()
	if err != nil {
		ErrorLogger.Printf("Unable to write output for device %q to file %q\n", hostname, f.Name())
		return err
	}
	return nil
}

// this func looks for error in CLI output (string started with %s). Returns
// string with error and bool if error found
func detectCliErrors(input string) (string, bool) {
	rows := strings.Split(input, "\n")
	var cliErr string
	var errFound bool
	for _, r := range rows {
		if strings.HasPrefix(r, "%") {
			cliErr = r
			errFound = true
			break
		}
	}
	return cliErr, errFound
}
