package main

import (
	"encoding/json"
	"flag"
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

func execCommand(args []string) error {
	if ok, _ := common.Exists(args[0]); !ok {
		return fmt.Errorf("path %s does not exist", args[0])
	}

	cmd := exec.Command(args[0], args[1:]...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func runCommand(script string) error {
	if strings.HasPrefix(script, "/builder") || strings.HasPrefix(script, "/init") {
		tokens, err := shlex.Split(script, true)
		if err != nil {
			return err
		}

		return execCommand(tokens)
	} else if script == "interactive" {
		return execCommand([]string{"/bin/login", "-f", "root"})
	} else {
		return execCommand([]string{"/bin/sh", "-c", script})
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
	Kind     string   `json:"kind"`
	Triggers []string `json:"triggers"`
	Exec     string   `json:"exec"`
}

func runScript(script BuilderScript) error {
	switch script.Kind {
	case "trigger_on":
		start := time.Now()

		for _, trigger := range script.Triggers {
			if ok, _ := common.Exists(trigger); !ok {
				continue
			}

			if err := execCommand([]string{script.Exec, trigger}); err != nil {
				return err
			}
		}

		slog.Info("trigger_on", "exec", script.Exec, "took", time.Since(start))

		return nil
	case "execute":
		start := time.Now()

		if err := execCommand([]string{script.Exec}); err != nil {
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

var (
	runScripts = flag.String("runScripts", "", "run a JSON file of scripts rather than /builder.json")
)

func builderMain() error {
	flag.Parse()

	if *runScripts != "" {
		return builderRunScripts(*runScripts)
	}

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
