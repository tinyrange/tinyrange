package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"go.starlark.net/starlark"
)

var KERNEL_URL = "https://github.com/tinyrange/linux_build/releases/download/linux_x86_6.6.7/vmlinux_x86_64"

func runTinyRange(exe string, configFilename string) (*exec.Cmd, error) {
	cmd := exec.Command(exe, "-config", configFilename)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.Info("executing tinyrange", "args", cmd.Args)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

func directiveToFragments(ctx common.BuildContext, directive common.Directive) ([]config.Fragment, error) {
	var ret []config.Fragment

	switch directive := directive.(type) {
	case *FetchOciImageDefinition:
		res, err := ctx.BuildChild(directive)
		if err != nil {
			return nil, err
		}

		if err := parseJsonFromFile(res, &directive); err != nil {
			return nil, err
		}

		for _, archive := range directive.LayerArchives {
			filename, err := ctx.FilenameFromDigest(archive)
			if err != nil {
				return nil, err
			}

			ret = append(ret, config.Fragment{Archive: &config.ArchiveFragment{HostFilename: filename}})
		}
	case common.DirectiveRunCommand:
		ret = append(ret, config.Fragment{RunCommand: &config.RunCommandFragment{Command: string(directive)}})
	case *StarBuildDefinition:
		res, err := ctx.BuildChild(directive)
		if err != nil {
			return nil, err
		}

		digest := res.Digest()

		filename, err := ctx.FilenameFromDigest(digest)
		if err != nil {
			return nil, err
		}

		ret = append(ret, config.Fragment{Archive: &config.ArchiveFragment{HostFilename: filename}})
	case *ReadArchiveBuildDefinition:
		res, err := ctx.BuildChild(directive)
		if err != nil {
			return nil, err
		}

		digest := res.Digest()

		filename, err := ctx.FilenameFromDigest(digest)
		if err != nil {
			return nil, err
		}

		ret = append(ret, config.Fragment{Archive: &config.ArchiveFragment{HostFilename: filename}})
	case *PlanDefinition:
		res, err := ctx.BuildChild(directive)
		if err != nil {
			return nil, err
		}

		if err := parseJsonFromFile(res, &directive); err != nil {
			return nil, err
		}

		ret = append(ret, directive.Fragments...)
	default:
		return nil, fmt.Errorf("BuildVmDefinition.Build: directive type %T unhandled", directive)
	}

	return ret, nil
}

type BuildVmDefinition struct {
	// TOOD(joshua): Allow customizing the kernel, hypervisor, and startup script.
	Directives  []common.Directive
	OutputFile  string
	StorageSize int

	mux    *http.ServeMux
	server *http.Server
	cmd    *exec.Cmd
	out    io.WriteCloser
}

// WriteTo implements common.BuildResult.
func (def *BuildVmDefinition) WriteTo(w io.Writer) (n int64, err error) {
	if err := def.cmd.Wait(); err != nil {
		return 0, err
	}

	def.server.Shutdown(context.Background())

	def.out.Close()

	return 0, nil
}

// Build implements common.BuildDefinition.
func (def *BuildVmDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	builderCfg := config.BuilderConfig{}

	builderCfg.OutputFilename = def.OutputFile

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}

	builderCfg.HostAddress = fmt.Sprintf("10.42.0.100:%d", listener.Addr().(*net.TCPAddr).Port)

	vmCfg := config.TinyRangeConfig{}

	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	kernel, err := ctx.BuildChild(NewFetchHttpBuildDefinition(KERNEL_URL, 0))
	if err != nil {
		return nil, err
	}

	kernelFilename, err := ctx.FilenameFromDigest(kernel.Digest())
	if err != nil {
		return nil, err
	}

	vmCfg.BaseDirectory = wd
	vmCfg.HypervisorScript = filepath.Join("hv/qemu/qemu.star")
	vmCfg.KernelFilename = kernelFilename
	vmCfg.StorageSize = def.StorageSize

	// Hard code the init file and script.
	vmCfg.RootFsFragments = append(vmCfg.RootFsFragments,
		config.Fragment{LocalFile: &config.LocalFileFragment{
			HostFilename:  filepath.Join("build/init_x86_64"),
			GuestFilename: "/init",
			Executable:    true,
		}},
		config.Fragment{LocalFile: &config.LocalFileFragment{
			HostFilename:  filepath.Join("cmd/tinyrange/init.star"),
			GuestFilename: "/init.star",
		}},
		// Use init.json to set /builder as the SSH command.
		config.Fragment{FileContents: &config.FileContentsFragment{
			Contents:      []byte("{\"ssh_command\": [\"/builder\"]}"),
			GuestFilename: "/init.json",
		}},
		// Send the local builder executable.
		config.Fragment{LocalFile: &config.LocalFileFragment{
			HostFilename:  "build/builder",
			GuestFilename: "/builder",
			Executable:    true,
		}},
	)

	// Launch child builds for each directive.
	for _, directive := range def.Directives {
		frags, err := directiveToFragments(ctx, directive)
		if err != nil {
			return nil, err
		}

		for _, frag := range frags {
			if frag.RunCommand != nil {
				builderCfg.Commands = append(builderCfg.Commands, frag.RunCommand.Command)
			} else {
				vmCfg.RootFsFragments = append(vmCfg.RootFsFragments, frag)
			}
		}
	}

	buildConfig, err := json.Marshal(&builderCfg)
	if err != nil {
		return nil, err
	}

	vmCfg.RootFsFragments = append(vmCfg.RootFsFragments,
		config.Fragment{FileContents: &config.FileContentsFragment{
			Contents:      buildConfig,
			GuestFilename: "/builder.json",
		}},
	)

	def.mux = http.NewServeMux()

	def.server = &http.Server{
		Handler: def.mux,
	}

	out, err := ctx.CreateOutput()
	if err != nil {
		return nil, err
	}
	def.out = out

	def.mux.HandleFunc("/upload_output", func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(def.out, r.Body)
		if err != nil {
			slog.Error("error writing output from VM", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	go func() {
		def.server.Serve(listener)
	}()

	configFilename, out, err := ctx.CreateFile(".json")
	if err != nil {
		return nil, err
	}

	enc := json.NewEncoder(out)

	if err := enc.Encode(&vmCfg); err != nil {
		out.Close()
		return nil, err
	}

	if err := out.Close(); err != nil {
		return nil, err
	}

	cmd, err := runTinyRange(filepath.Join(wd, "build/tinyrange"), configFilename)
	if err != nil {
		return nil, err
	}

	def.cmd = cmd

	return def, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *BuildVmDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.Database().ShouldRebuildUserDefinitions() {
		return true, nil
	}

	// TODO(joshua): Check if any of the child directives
	return false, nil
}

// Tag implements common.BuildDefinition.
func (def *BuildVmDefinition) Tag() string {
	out := []string{"BuildVm"}

	for _, dir := range def.Directives {
		out = append(out, dir.Tag())
	}

	out = append(out, def.OutputFile)

	return strings.Join(out, "_")
}

func (def *BuildVmDefinition) String() string { return def.Tag() }
func (*BuildVmDefinition) Type() string       { return "BuildVmDefinition" }
func (*BuildVmDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("BuildVmDefinition is not hashable")
}
func (*BuildVmDefinition) Truth() starlark.Bool { return starlark.True }
func (*BuildVmDefinition) Freeze()              {}

var (
	_ starlark.Value         = &BuildVmDefinition{}
	_ common.BuildDefinition = &BuildVmDefinition{}
	_ common.BuildResult     = &BuildVmDefinition{}
)

func NewBuildVmDefinition(dir []common.Directive, output string, storageSize int) *BuildVmDefinition {
	if storageSize == 0 {
		storageSize = 1024
	}
	return &BuildVmDefinition{Directives: dir, OutputFile: output, StorageSize: storageSize}
}
