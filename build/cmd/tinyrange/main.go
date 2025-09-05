package main

import (
	"log/slog"
	"os"

	"github.com/tinyrange/tinyrange/build"
)

func main() {
	if err := build.Main(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
