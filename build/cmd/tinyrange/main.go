package main

import (
	"log/slog"
	"os"

	build "github.com/tinyrange/tinyrange/build/cli"
)

func main() {
	if err := build.Main(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
