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
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"go.starlark.net/starlark"
)

var OFFICIAL_KERNEL_URL = "https://github.com/tinyrange/linux_build/releases/download/linux_x86_6.6.7/vmlinux_x86_64"

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

		if err := ParseJsonFromFile(res, &directive); err != nil {
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
	case *CreateArchiveDefinition:
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
	case *FileDefinition:
		res, err := ctx.BuildChild(directive)
		if err != nil {
			return nil, err
		}

		digest := res.Digest()

		filename, err := ctx.FilenameFromDigest(digest)
		if err != nil {
			return nil, err
		}

		stat, err := directive.f.Stat()
		if err != nil {
			return nil, err
		}

		ret = append(ret, config.Fragment{LocalFile: &config.LocalFileFragment{
			HostFilename:  filename,
			GuestFilename: stat.Name(),
			Executable:    stat.Mode().Perm()&0111 != 0,
		}})
	case *PlanDefinition:
		res, err := ctx.BuildChild(directive)
		if err != nil {
			return nil, err
		}

		if err := ParseJsonFromFile(res, &directive); err != nil {
			return nil, err
		}

		ret = append(ret, directive.Fragments...)
	case common.DirectiveArchive:
		res, err := ctx.BuildChild(directive.Definition)
		if err != nil {
			return nil, err
		}

		digest := res.Digest()

		filename, err := ctx.FilenameFromDigest(digest)
		if err != nil {
			return nil, err
		}

		ret = append(ret, config.Fragment{Archive: &config.ArchiveFragment{
			HostFilename: filename,
			Target:       directive.Target,
		}})
	case common.DirectiveAddFile:
		if directive.Definition != nil {
			res, err := ctx.BuildChild(directive.Definition)
			if err != nil {
				return nil, err
			}

			digest := res.Digest()

			filename, err := ctx.FilenameFromDigest(digest)
			if err != nil {
				return nil, err
			}

			ret = append(ret, config.Fragment{LocalFile: &config.LocalFileFragment{
				GuestFilename: directive.Filename,
				HostFilename:  filename,
				Executable:    directive.Executable,
			}})
		} else {
			ret = append(ret, config.Fragment{FileContents: &config.FileContentsFragment{
				GuestFilename: directive.Filename,
				Contents:      directive.Contents,
				Executable:    directive.Executable,
			}})
		}
	default:
		return nil, fmt.Errorf("BuildVmDefinition.Build: directive type %T unhandled", directive)
	}

	return ret, nil
}

type BuildVmDefinition struct {
	params BuildVmParameters

	mux       *http.ServeMux
	server    *http.Server
	cmd       *exec.Cmd
	out       io.WriteCloser
	gotOutput bool
}

// Create implements common.BuildDefinition.
func (def *BuildVmDefinition) Create(params common.BuildDefinitionParameters) common.BuildDefinition {
	return &BuildVmDefinition{params: *params.(*BuildVmParameters)}
}

// Params implements common.BuildDefinition.
func (def *BuildVmDefinition) Params() common.BuildDefinitionParameters {
	return def.params
}

// WriteTo implements common.BuildResult.
func (def *BuildVmDefinition) WriteTo(w io.Writer) (n int64, err error) {
	if err := def.cmd.Wait(); err != nil {
		return 0, err
	}

	if !def.gotOutput && def.params.OutputFile != "" {
		return 0, fmt.Errorf("VM did not write any output")
	}

	def.server.Shutdown(context.Background())

	def.out.Close()

	return 0, nil
}

// Build implements common.BuildDefinition.
func (def *BuildVmDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	builderCfg := config.BuilderConfig{}

	builderCfg.OutputFilename = def.params.OutputFile

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

	kernelDef := def.params.Kernel
	if kernelDef == nil {
		kernelDef = NewFetchHttpBuildDefinition(OFFICIAL_KERNEL_URL, 0)
	}

	kernel, err := ctx.BuildChild(kernelDef)
	if err != nil {
		return nil, err
	}

	kernelFilename, err := ctx.FilenameFromDigest(kernel.Digest())
	if err != nil {
		return nil, err
	}

	hvScript, err := common.GetAdjacentExecutable("tinyrange_qemu.star")
	if err != nil {
		return nil, err
	}

	vmCfg.BaseDirectory = wd
	vmCfg.HypervisorScript = hvScript
	vmCfg.KernelFilename = kernelFilename
	vmCfg.CPUCores = def.params.CpuCores
	vmCfg.MemoryMB = def.params.MemoryMB
	vmCfg.StorageSize = def.params.StorageSize
	vmCfg.Interaction = def.params.Interaction

	if def.params.InitRamFs != nil {
		// bypass the default init logic.
		// The user code is expected to call `/init -run-config /builder.json` some how.

		initRamFs, err := ctx.BuildChild(def.params.InitRamFs)
		if err != nil {
			return nil, err
		}

		initRamFsFilename, err := ctx.FilenameFromDigest(initRamFs.Digest())
		if err != nil {
			return nil, err
		}

		vmCfg.InitFilesystemFilename = initRamFsFilename
	}

	// Hard code the init file and script.
	vmCfg.RootFsFragments = append(vmCfg.RootFsFragments,
		config.Fragment{Builtin: &config.BuiltinFragment{Name: "init", GuestFilename: "/init"}},
		config.Fragment{Builtin: &config.BuiltinFragment{Name: "init.star", GuestFilename: "/init.star"}},
		// Use init.json to set the builder entry point as the SSH command.
		config.Fragment{FileContents: &config.FileContentsFragment{
			Contents:      []byte("{\"ssh_command\": [\"/init\", \"-run-config\", \"/builder.json\"]}"),
			GuestFilename: "/init.json",
		}},
	)

	// Launch child builds for each directive.
	for _, directive := range def.params.Directives {
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
		def.gotOutput = true

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

	exe, err := common.GetTinyRangeExecutable()
	if err != nil {
		return nil, err
	}

	cmd, err := runTinyRange(exe, configFilename)
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

func (def *BuildVmDefinition) String() string { return "BuildVmDefinition" }
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

func NewBuildVmDefinition(
	dir []common.Directive,
	kernel common.BuildDefinition,
	initramfs common.BuildDefinition,
	output string,
	cpuCores int,
	memoryMb int,
	storageSize int,
	interaction string,
) *BuildVmDefinition {
	if storageSize == 0 {
		storageSize = 1024
	}
	if cpuCores == 0 {
		cpuCores = 1
	}
	if memoryMb == 0 {
		memoryMb = 1024
	}
	return &BuildVmDefinition{
		params: BuildVmParameters{
			Directives:  dir,
			Kernel:      kernel,
			InitRamFs:   initramfs,
			OutputFile:  output,
			CpuCores:    cpuCores,
			MemoryMB:    memoryMb,
			StorageSize: storageSize,
			Interaction: interaction,
		},
	}
}
