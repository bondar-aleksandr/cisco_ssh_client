package main

import (
	"bufio"
	"os"
)

// func received list of Devices, walk through it, finds unique filenames,  and populates
// cmdCache variable with mapping
func BuildCmdCache(entries []*Device) {
	for _, entry := range entries {
		commandsFile, err := os.Open(entry.CmdFile)
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
