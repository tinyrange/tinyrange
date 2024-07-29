package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/anmitsu/go-shlex"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
)

func execCommand(args []string, environment map[string]string) error {
	if ok, _ := common.Exists(args[0]); !ok {
		return fmt.Errorf("path %s does not exist", args[0])
	}

	cmd := exec.Command(args[0], args[1:]...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = cmd.Environ()

	for k, v := range environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	return cmd.Run()
}

func runCommand(script string) error {
	if strings.HasPrefix(script, "/init") {
		tokens, err := shlex.Split(script, true)
		if err != nil {
			return err
		}

		return execCommand(tokens, nil)
	} else if script == "interactive" {
		return execCommand([]string{"/bin/login", "-f", "root"}, nil)
	} else {
		return execCommand([]string{"/bin/sh", "-c", script}, nil)
	}
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

			if err := execCommand([]string{script.Exec, trigger}, script.Environment); err != nil {
				return err
			}
		}

		slog.Info("trigger_on", "exec", script.Exec, "took", time.Since(start))

		return nil
	case "execute":
		start := time.Now()

		if err := execCommand(append([]string{script.Exec}, script.Arguments...), script.Environment); err != nil {
			return err
		}

		slog.Info("ran script", "exec", script.Exec, "took", time.Since(start))

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
