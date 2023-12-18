package device

import (

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

const (
	Ok                   = "Success"
	Unreachable          = "Unreachable"
	Unknown              = "Unknown"
	SshAuthFailure       = "SSH authentication failure"
	PermissionProblem    = "Permission problem/Canceled"
	CmdPartiallyAccepted = "Commands accepted with errors"
	SaveFailed			 = "Commands accepted, save config failed"
)

// type describes cli error
type DevError struct {
	Device string
	Cmd    string
	Msg    string
}