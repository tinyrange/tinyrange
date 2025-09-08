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
	"strings"
)

type buildContext struct {
	basePath    string
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

func (ctx *buildContext) buildGo(packageName string, outName string, buildSettings goBuildSettings) (string, error) {
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

func (ctx *buildContext) buildProto(outputDir string, inputs ...string) error {
	if _, err := ctx.checkExecutableExists("protoc"); err != nil {
		return err
	}

	if _, err := ctx.checkExecutableExists("protoc-gen-go"); err != nil {
		return fmt.Errorf("protoc-gen-go not found in PATH, please install it with 'go install google.golang.org/protobuf/cmd/protoc-gen-go@latest'")
	}

	// Determine module path from go.mod for import paths
	gomodData, err := os.ReadFile(filepath.Join(ctx.basePath, "go.mod"))
	if err != nil {
		return fmt.Errorf("failed to read go.mod: %w", err)
	}

	modulePath := ""
	for line := range strings.SplitSeq(string(gomodData), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "module "); ok {
			modulePath = strings.TrimSpace(after)
			break
		}
	}
	if modulePath == "" {
		return fmt.Errorf("failed to determine module path from go.mod")
	}

	// Build go_opt mappings: M<file>=<module>/<outDir>/<proto_package>
	var goOpts []string
	for _, input := range inputs {
		// Parse package name from file
		contents, err := os.ReadFile(input)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", input, err)
		}
		pkgName := ""
		for l := range strings.SplitSeq(string(contents), "\n") {
			l = strings.TrimSpace(l)
			if after, ok := strings.CutPrefix(l, "package "); ok {
				// e.g. package common;
				pkgName = strings.TrimSuffix(strings.TrimSpace(after), ";")
				break
			}
		}
		if pkgName == "" {
			return fmt.Errorf("missing package declaration in %s", input)
		}

		// Map this file's import path so imports resolve without go_package options
		goOpts = append(goOpts, fmt.Sprintf("M%[1]s=%[2]s/%[3]s/%[4]s",
			filepath.Base(input), modulePath, strings.TrimSuffix(outputDir, string(filepath.Separator)), pkgName))
	}

	// Construct protoc command
	args := []string{
		"-I", filepath.Dir(inputs[0]),
		fmt.Sprintf("--go_out=paths=source_relative:%s", outputDir),
	}
	for _, opt := range goOpts {
		args = append(args, "--go_opt="+opt)
	}
	args = append(args, inputs...)

	cmd := exec.Command("protoc", args...)
	cmd.Dir = ctx.basePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("protoc command failed: %w", err)
	}

	return nil
}

func main() {
	fs := flag.NewFlagSet("build", flag.ExitOnError)

	run := fs.Bool("run", false, "Run the built binary after building")
	proto := fs.Bool("proto", false, "Build protobuf files")

	fs.Parse(os.Args[1:])

	cwd, err := os.Getwd()
	if err != nil {
		slog.Error("failed to get current working directory", "error", err)
		os.Exit(1)
	}

	ctx := &buildContext{
		basePath:    cwd,
		basePackage: "github.com/tinyrange/tinyrange",
		buildDir:    "local/build",
	}

	goSettings := ctx.defaultGoSettings()

	if *proto {
		slog.Info("building protobuf files")
		if err := ctx.buildProto("build/proto",
			"build/proto/build.proto",
			"build/proto/fetch_http.proto",
			"build/proto/extract_archive.proto",
			"build/proto/source.proto",
			"build/proto/write_file.proto",
		); err != nil {
			slog.Error("protoc failed", "error", err)
			os.Exit(1)
		}
	}

	slog.Info("building tinyrange", "os", goSettings.os, "arch", goSettings.arch)
	outFilename, err := ctx.buildGo("cmd/tinyrange", "tinyrange", ctx.defaultGoSettings())
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
			os.Exit(1)
		}
	}
}
