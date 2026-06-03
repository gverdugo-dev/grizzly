package main

import (
	"fmt"
	"log/slog"

	"grizzly"
	"grizzly/internal/logging"
)

func main() {
	logger := logging.New(slog.LevelDebug)

	logger.Info("Testing grizzly dataframes")
	logger.Debug("debug")
	logger.Warn("warn")
	logger.Error("error")
	d := grizzly.NewDataframe()
	fmt.Println(d)
}
