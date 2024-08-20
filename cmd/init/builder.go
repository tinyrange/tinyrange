package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
)

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
	for _, env := range cfg.Environment {
		k, v, _ := strings.Cut(env, "=")
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}

	for _, cmd := range cfg.Commands {
		slog.Debug("running", "cmd", cmd)
		if err := common.RunCommand(cmd); err != nil {
			return err
		}
	}

	if cfg.OutputFilename != "" {
		if err := uploadFile(cfg.HostAddress, cfg.OutputFilename); err != nil {
			return err
		}
	}

	return nil
}

type BuilderScript struct {
	Kind        string            `json:"kind"`
	Triggers    []string          `json:"triggers"`
	Exec        string            `json:"exec"`
	Arguments   []string          `json:"args"`
	Environment map[string]string `json:"env"`
}

func runScript(script BuilderScript) error {
	switch script.Kind {
	case "trigger_on":
		start := time.Now()

		for _, trigger := range script.Triggers {
			if ok, _ := common.Exists(trigger); !ok {
				continue
			}

			if err := common.ExecCommand([]string{script.Exec, trigger}, script.Environment); err != nil {
				return fmt.Errorf("failed to run trigger: %s", err)
			}
		}

		slog.Debug("trigger_on", "exec", script.Exec, "took", time.Since(start))

		return nil
	case "execute":
		start := time.Now()

		if err := common.ExecCommand(append([]string{script.Exec}, script.Arguments...), script.Environment); err != nil {
			return fmt.Errorf("failed to run command (%s): %s", script.Exec, err)
		}

		slog.Debug("ran script", "exec", script.Exec, "took", time.Since(start))

		return nil
	default:
		return fmt.Errorf("unknown kind: %s", script.Kind)
	}
}

func builderRunScripts(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(f)

	var scripts []BuilderScript

	if err := dec.Decode(&scripts); err != nil {
		return err
	}

	for _, script := range scripts {
		if err := runScript(script); err != nil {
			return err
		}
	}

	return nil
}
