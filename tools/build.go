///usr/bin/true; exec /usr/bin/env go run "$0" "$@"

package main

import (
	"flag"
	"fmt"
	"io/fs"
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

func (ctx *buildContext) exists(path string) (fs.FileInfo, error) {
	fullPath := filepath.Join(ctx.basePath, path)
	return os.Stat(fullPath)
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

type buildProtoOptions struct {
	output     string
	golang     bool
	typeScript bool
	grpc       bool
}

func (ctx *buildContext) buildProto(options buildProtoOptions, inputs ...string) error {
	if _, err := ctx.checkExecutableExists("protoc"); err != nil {
		return err
	}

	if _, err := ctx.checkExecutableExists("protoc-gen-go"); err != nil {
		return fmt.Errorf("protoc-gen-go not found in PATH, please install it with 'go install google.golang.org/protobuf/cmd/protoc-gen-go@latest'")
	}

	if options.grpc {
		if _, err := ctx.checkExecutableExists("protoc-gen-go-grpc"); err != nil {
			return fmt.Errorf("protoc-gen-go-grpc not found in PATH, please install it with 'go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest'")
		}

		if !options.golang {
			return fmt.Errorf("grpc option requires golang option to be set")
		}
	}

	tsProtoPath := filepath.Join("vibe_party", "web", "node_modules", ".bin", "protoc-gen-ts_proto")
	if options.typeScript {
		if _, err := ctx.exists(tsProtoPath); err != nil {
			return fmt.Errorf("protoc-gen-ts_proto not found at %s, please install it with 'bun install'", tsProtoPath)
		}
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

	// Construct protoc command
	args := []string{
		"-I", ".",
	}

	if options.golang {
		args = append(args, "--go_out=paths=source_relative:.")
	}

	if options.typeScript {
		args = append(args,
			fmt.Sprintf("--plugin=%s", tsProtoPath),
			fmt.Sprintf("--ts_proto_out=%s", filepath.Join(ctx.basePath, options.output)),
			"--ts_proto_opt=esModuleInterop=true",
			"--ts_proto_opt=forceLong=string",
		)
	}

	if options.grpc {
		args = append(args,
			fmt.Sprintf("--go-grpc_out=paths=source_relative:%s", options.output),
		)
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
	test := fs.Bool("test", false, "Run tests after building")
	proto := fs.Bool("proto", false, "Build protobuf files")
	fmt := fs.Bool("fmt", false, "Run go fmt on all .go files")

	fs.Parse(os.Args[1:])

	if *fmt {
		slog.Info("running go fmt")
		cmd := exec.Command("go", "fmt", "./...")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err != nil {
			slog.Error("go fmt failed", "error", err)
			os.Exit(1)
		}

		return
	} else if *test {
		slog.Info("running tests")
		cmd := exec.Command("go", "test", "./...")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
	}

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
		if err := ctx.buildProto(buildProtoOptions{
			output: "build/proto",
			golang: true,
		},
			// basic build system
			"build/proto/build.proto",

			// definitions
			"build/proto/fetch_http.proto",
			"build/proto/extract_archive.proto",
			"build/proto/source.proto",
			"build/proto/write_file.proto",
		); err != nil {
			slog.Error("protoc failed", "error", err)
			os.Exit(1)
		}

		if err := ctx.buildProto(buildProtoOptions{
			output: "machine/proto",
			golang: true,
		},
			"machine/proto/definition.proto",
			"machine/proto/directive.proto",
		); err != nil {
			slog.Error("protoc failed", "error", err)
			os.Exit(1)
		}

		if err := ctx.buildProto(buildProtoOptions{
			output:     "vibe_party/web/src/gen",
			typeScript: true,
		},
			// basic build system
			"build/proto/build.proto",
		); err != nil {
			slog.Error("protoc failed", "error", err)
			os.Exit(1)
		}

		if err := ctx.buildProto(buildProtoOptions{
			output: "vibe_party/localclient/proto",
			golang: true,
			grpc:   true,
		},
			// machine interface
			"vibe_party/localclient/proto/machine.proto",
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
