package main

import (
)

// describes entry in csv device file
type Device struct {
	Hostname  string `csv:"hostname"`
	Login     string `csv:"login"`
	Password  string `csv:"password"`
	OsType    string `csv:"osType"`
	Configure bool   `csv:"configure"`
	SaveConfig	  bool   `csv:"saveConfig"`
	CmdFile   string `csv:"cmdFile"`
	State     string
}

// returns CLI command for saving configuration, platform dependend
func(d *Device) GetSaveCommand() (string) {
	var cmd string
	switch d.OsType {
	case "ios":
		cmd = "wr mem"
	case "nxos":
		cmd = "copy run start"
	}
	return cmd
}

// type for app-level config
type config struct {
	Client struct  {
		SSHTimeout int64 `yaml:"ssh_timeout"`
		LegacyKeyExchange string `yaml:"legacy_key_exchange"`
		LegacyAlgorithm string `yaml:"legacy_algorithm"`
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
type devError struct {
	device string
	cmd    string
	msg    string
}