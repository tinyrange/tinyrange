package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
)

// Grab scripts with: ./tools/build.go -run -- build scripts/get_scripts.star:script_fs -o local/scripts.tar
// Test with: find local/scripts | grep "\.sh" | xargs go run github.com/tinyrange/tinyrange/experimental/shToStar

func translateFile(input string) ([]byte, error) {
	f, err := os.Open(input)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sh := NewTranspiler()

	return sh.TranslateFile(f, input)
}

func runScript(filename string, contents []byte) error {
	rt := NewRuntime()

	return rt.Run(filename, contents)
}

var (
	runScripts = flag.Bool("run", false, "run the scripts (without actually running shell commands)")
)

func appMain() error {
	flag.Parse()

	w := os.Stderr

	slog.SetDefault(slog.New(
		tint.NewHandler(w, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.RFC3339Nano,
			NoColor:    !isatty.IsTerminal(w.Fd()),
		}),
	))

	var (
		successes []string
		failures  []string
	)

	for _, input := range flag.Args() {
		// slog.Info("", "in", input)
		out, err := translateFile(input)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s %s", input, err))
		} else {
			successes = append(successes, input)

			if true {
				slog.Info("success", "input", input)
				os.Stdout.Write(out)
			}
		}

		if *runScripts {
			if err := runScript(input, out); err != nil {
				return err
			}
		}
	}

	for _, failure := range failures {
		slog.Error("failure", "fail", failure)
	}

	slog.Info("results", "successes", len(successes), "failures", len(failures))

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
