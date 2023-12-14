package main

import (
	"bufio"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"github.com/bondar-aleksandr/cisco_ssh_client/logger"
)

// func receives list of Devices, walk through it, finds unique filenames, and populates
// cmdCache variable with mapping filename:Commands
func buildCmdCache(entries []*Device) {
	logger.L.Info("Building cmd cache...")

	for _, entry := range entries {
		commandsFile, err := os.Open(filepath.Join(appConfig.Data.InputFolder, entry.CmdFile))
		if err != nil {
			logger.L.Error("Unable to open commands file", "file", entry.CmdFile)
			os.Exit(1)
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
	logger.L.Info("Building cmd cache done")
}

// this func Unmarshals config.yml content to config variable
func readConfig(cfg *config) {
	logger.L.Info("Reading config...")

	f, err := os.Open(configPath)
	if err != nil {
		logger.L.Error("Cannot read app config file", "error", err.Error())
		os.Exit(1)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	err = decoder.Decode(cfg)
	if err != nil {
		logger.L.Error("Cannot parse app config file", "error", err.Error())
		os.Exit(1)
	}
	logger.L.Info("Reading config done")
	//TODO: print config parameters
}


// this func looks for error in CLI output. Returns
// string with error and bool if error found
func detectCliErrors(input string) (string, bool) {
	rows := strings.Split(input, "\n")
	var cliErr string
	var errFound bool
	for _, r := range rows {
		if strings.HasPrefix(r, "%") || strings.HasPrefix(r, "Command rejected:") {
			cliErr = r
			errFound = true
			break
		}
	}
	return cliErr, errFound
}

// this func creates directory for storing outputs if it doesn't exists before
func prepareDirectory() error {
	//create folder for outputs if not exists
	logger.L.Info("Creating output directory is not exists...")
	outDir := filepath.Join(appConfig.Data.OutputFolder)
	_, err := os.Stat(outDir)

	if os.IsNotExist(err) {
		errDir := os.MkdirAll(appConfig.Data.OutputFolder, os.ModePerm)
		if errDir != nil {
			return err
		}
		logger.L.Info("Created output directory successfully", "directory", outDir)
	} else {
		logger.L.Info("Output directory already there")
	}
	return nil
}