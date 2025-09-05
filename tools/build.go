///usr/bin/true; exec /usr/bin/env go run "$0" "$@"

package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type buildContext struct {
	basePackage string
	buildDir    string
}

type goBuildSettings struct {
	goCommand string
	os        string
	arch      string
}

func (ctx *buildContext) checkExecutableExists(name string) (string, error) {
	lookup, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return lookup, nil
}

func (ctx *buildContext) ensureBuildDir() error {
	if ctx.buildDir == "" {
		return fmt.Errorf("build directory not set")
	}

	return os.MkdirAll(ctx.buildDir, 0755)
}

func (ctx *buildContext) getExecutableTarget(baseName string, os string) (string, error) {
	if err := ctx.ensureBuildDir(); err != nil {
		return "", err
	}

	if os == "windows" {
		baseName += ".exe"
	}

	return filepath.Join(ctx.buildDir, baseName), nil
}

func (ctx *buildContext) defaultGoSettings() goBuildSettings {
	return goBuildSettings{
		goCommand: "go",
		os:        runtime.GOOS,
		arch:      runtime.GOARCH,
	}
}

func (ctx *buildContext) goBuild(packageName string, outName string, buildSettings goBuildSettings) (string, error) {
	if _, err := ctx.checkExecutableExists(buildSettings.goCommand); err != nil {
		return "", err
	}

	outFilename, err := ctx.getExecutableTarget(outName, buildSettings.os)
	if err != nil {
		return "", err
	}

	packageName = ctx.basePackage + "/" + packageName

	cmd := exec.Command(buildSettings.goCommand, "build", "-o", outFilename, packageName)
	cmd.Env = append(os.Environ(),
		"GOOS="+buildSettings.os,
		"GOARCH="+buildSettings.arch,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", err
	}

	return outFilename, nil
}

func main() {
	fs := flag.NewFlagSet("build", flag.ExitOnError)

	run := fs.Bool("run", false, "Run the built binary after building")

	fs.Parse(os.Args[1:])

	ctx := &buildContext{
		basePackage: "github.com/tinyrange/tinyrange",
		buildDir:    "local/build",
	}

	goSettings := ctx.defaultGoSettings()

	slog.Info("building tinyrange", "os", goSettings.os, "arch", goSettings.arch)
	outFilename, err := ctx.goBuild("build/cmd/tinyrange", "tinyrange", ctx.defaultGoSettings())
	if err != nil {
		slog.Error("build failed", "error", err)
		os.Exit(1)
	}

	if *run {
		cmd := exec.Command(outFilename, fs.Args()...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err != nil {
			slog.Error("execution failed", "error", err)
			os.Exit(1)
		}
	}
}
