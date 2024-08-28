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
	shelltranslater "github.com/tinyrange/tinyrange/pkg/shellTranslater"
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

func translateAndRun(args []string, environment map[string]string) (bool, error) {
	transpileStart := time.Now()

	f, err := os.Open(args[0])
	if err != nil {
		return false, err
	}
	defer f.Close()

	sh := shelltranslater.NewTranspiler(true)

	translated, err := sh.TranslateFile(f, args[0])
	if err != nil {
		return false, err
	}

	transpileTime := time.Since(transpileStart)

	runStart := time.Now()

	rt := shelltranslater.NewRuntime(true)

	slog.Debug("running translated", "prog", args[0])

	if err := rt.Run(args[0], translated, args, environment); err != nil {
		return true, err
	}

	runTime := time.Since(runStart)

	slog.Debug("translated and run", "prog", args[0], "transpile", transpileTime, "run", runTime)

	return true, nil
}

func execCommand(translateShell bool, args []string, env map[string]string) error {
	if translateShell {
		fatal, err := translateAndRun(args, env)
		if err != nil {
			if fatal {
				return fmt.Errorf("failed to translate and run: %s", err)
			} else {
				slog.Warn("failed to translate", "err", err)
			}
		} else {
			return nil
		}
	}

	if err := common.ExecCommand(args, env); err != nil {
		return fmt.Errorf("failed to run trigger: %s", err)
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

func runScript(script BuilderScript, translateShell bool) error {
	switch script.Kind {
	case "trigger_on":
		start := time.Now()

		args := []string{}

		for _, trigger := range script.Triggers {
			if ok, _ := common.Exists(trigger); !ok {
				continue
			}

			args = append(args, trigger)
		}

		if len(args) == 0 {
			return nil
		}

		if err := execCommand(
			translateShell,
			append([]string{script.Exec}, args...),
			script.Environment,
		); err != nil {
			return err
		}

		slog.Debug("trigger_on", "exec", script.Exec, "took", time.Since(start))

		return nil
	case "execute":
		start := time.Now()

		if err := execCommand(
			translateShell,
			append([]string{script.Exec}, script.Arguments...),
			script.Environment,
		); err != nil {
			return err
		}

		slog.Debug("ran script", "exec", script.Exec, "took", time.Since(start))

		return nil
	default:
		return fmt.Errorf("unknown kind: %s", script.Kind)
	}
}

func builderRunScripts(filename string, translateShell bool) error {
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
		if err := runScript(script, translateShell); err != nil {
			return err
		}
	}

	return nil
}
