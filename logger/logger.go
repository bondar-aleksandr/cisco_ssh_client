package logger

import (
	"log/slog"
	"os"
)

var L *slog.Logger
var ProgramLevel = new(slog.LevelVar)

func init() {
	L = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: ProgramLevel, AddSource: true}))
}