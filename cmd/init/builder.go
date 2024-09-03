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

type Builder struct {
	translateShell bool

	totalTranslate     time.Duration
	totalRunTranslated time.Duration
	totalRunCommand    time.Duration
}

// OnBuiltin implements shelltranslater.Notifier.
func (b *Builder) OnBuiltin(name string, args []string) {
	if !common.IsVerbose() {
		return
	}

	fmt.Fprintf(os.Stderr, "||   builtin.%s(%+v)\n", name, args)
}

// PostRunShell implements shelltranslater.Notifier.
func (b *Builder) PostRunShell(args []string, exit int, took time.Duration) {
	if !common.IsVerbose() {
		return
	}

	fmt.Fprintf(os.Stderr, "||   < runShell(%+v) = %d [%s]\n", args, exit, took)
}

// PreRunShell implements shelltranslater.Notifier.
func (b *Builder) PreRunShell(args []string) {
	if !common.IsVerbose() {
		return
	}

	fmt.Fprintf(os.Stderr, "||   > runShell(%+v)\n", args)
}

func (b *Builder) uploadFile(address string, filename string) error {
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

func (b *Builder) translateAndRun(args []string, environment map[string]string) (bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	defer os.Chdir(cwd)

	transpileStart := time.Now()

	f, err := os.Open(args[0])
	if err != nil {
		return false, err
	}
	defer f.Close()

	sh := shelltranslater.NewTranspiler(true, true)

	translated, err := sh.TranslateFile(f, args[0])
	if err != nil {
		return false, err
	}

	transpileTime := time.Since(transpileStart)

	runStart := time.Now()

	rt := shelltranslater.NewRuntime(true, b)

	if common.IsVerbose() {
		fmt.Fprintf(os.Stderr, "|| > translated(%s)\n", args[0])
	}

	if err := rt.Run(args[0], translated, args, environment); err != nil {
		return true, err
	}

	runTime := time.Since(runStart)

	if common.IsVerbose() {
		fmt.Fprintf(os.Stderr, "|| < translated(%s) [transpile=%s, run=%s]\n", args[0], transpileTime, runTime)
		b.totalTranslate += transpileTime
		b.totalRunTranslated += runTime
	}

	return true, nil
}

func (b *Builder) execCommand(args []string, env map[string]string) error {
	if b.translateShell {
		fatal, err := b.translateAndRun(args, env)
		if err != nil {
			if fatal {
				return fmt.Errorf("failed to translate and run: %s", err)
			} else {
				if common.IsVerbose() {
					fmt.Fprintf(os.Stderr, "|| W translate(%s) = %s\n", args[0], err)
				}
			}
		} else {
			return nil
		}
	}

	start := time.Now()

	if err := common.ExecCommand(args, env); err != nil {
		return fmt.Errorf("failed to run command: %s", err)
	}

	if common.IsVerbose() {
		b.totalRunCommand += time.Since(start)
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

func (b *Builder) runScript(script BuilderScript) error {
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

		if err := b.execCommand(
			append([]string{script.Exec}, args...),
			script.Environment,
		); err != nil {
			return err
		}

		slog.Debug("trigger_on", "exec", script.Exec, "took", time.Since(start))

		return nil
	case "execute":
		start := time.Now()

		if common.IsVerbose() {
			fmt.Fprintf(os.Stderr, "|| > execute(%s, %+v)\n", script.Exec, script.Arguments)
		}

		if err := b.execCommand(
			append([]string{script.Exec}, script.Arguments...),
			script.Environment,
		); err != nil {
			return err
		}

		if common.IsVerbose() {
			fmt.Fprintf(os.Stderr, "|| < execute(%s, %+v) [%s]\n", script.Exec, script.Arguments, time.Since(start))
		}

		return nil
	default:
		return fmt.Errorf("unknown kind: %s", script.Kind)
	}
}

func (b *Builder) RunScripts(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(f)

	var scripts []BuilderScript

	if err := dec.Decode(&scripts); err != nil {
		return err
	}

	start := time.Now()

	if common.IsVerbose() {
		fmt.Fprintf(os.Stderr, "Started running %s at %s\nUsing Basic Environment:\n",
			filename, time.Now().Format(time.RFC1123))

		for _, val := range os.Environ() {
			fmt.Fprintf(os.Stderr, "- %s\n", val)
		}

		fmt.Fprintf(os.Stderr, "\n")
	}

	for _, script := range scripts {
		if err := b.runScript(script); err != nil {
			return err
		}

		if common.IsVerbose() {
			fmt.Fprintf(os.Stderr, "\n\n")
		}
	}

	if common.IsVerbose() {
		fmt.Fprintf(os.Stderr, "Finished running %s at %s [%s]\nTotal Translate Time: %s\nTotal Translated Runtime: %s\nTotal Regular Runtime: %s\n",
			filename, time.Now().Format(time.RFC1123), time.Since(start),
			b.totalTranslate, b.totalRunTranslated, b.totalRunCommand)
	}

	return nil
}

func builderRunScripts(filename string, translateShell bool) error {
	builder := &Builder{translateShell: translateShell}

	return builder.RunScripts(filename)
}

func builderRunWithConfig(cfg config.BuilderConfig) error {
	builder := &Builder{}

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
		if err := builder.uploadFile(cfg.HostAddress, cfg.OutputFilename); err != nil {
			return err
		}
	}

	return nil
}
