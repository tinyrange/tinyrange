package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"

	"github.com/tinyrange/pkg2/v2/builder/config"
)

func runCommand(script string) error {
	cmd := exec.Command("/bin/sh", "-c", script)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Don't attach stdin.

	return cmd.Run()
}

func uploadFile(address string, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	url := fmt.Sprintf("http://%s/upload_output", address)

	resp, err := http.Post(url, "application/binary", f)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func runWithConfig(cfg config.BuilderConfig) error {
	for _, cmd := range cfg.Commands {
		slog.Info("running", "cmd", cmd)
		if err := runCommand(cmd); err != nil {
			return err
		}
	}

	if err := uploadFile(cfg.HostAddress, cfg.OutputFilename); err != nil {
		return err
	}

	return nil
}

func builderMain() error {
	f, err := os.Open("/builder.json")
	if err != nil {
		return err
	}

	dec := json.NewDecoder(f)

	var cfg config.BuilderConfig

	if err := dec.Decode(&cfg); err != nil {
		return err
	}

	return runWithConfig(cfg)
}

func main() {
	if err := builderMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
