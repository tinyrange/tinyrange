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
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&BuildVmDefinition{})
}

type ErrTemplateBuilt string

// Error implements error.
func (e ErrTemplateBuilt) Error() string { return "template built" }

var (
	_ error = ErrTemplateBuilt("")
)

type BuildVmDefinition struct {
	params BuildVmParameters

	buildTemplateOutput bool

	mux       *http.ServeMux
	server    *http.Server
	cmd       *exec.Cmd
	out       io.WriteCloser
	gotOutput bool
}

func (def *BuildVmDefinition) SetBuildTemplateMode() {
	def.buildTemplateOutput = true
}

// Dependencies implements common.BuildDefinition.
func (def *BuildVmDefinition) Dependencies(ctx common.BuildContext) ([]common.DependencyNode, error) {
	var ret []common.DependencyNode

	arch, err := config.ArchitectureFromString(def.params.Architecture)
	if err != nil {
		return nil, err
	}
	if arch == config.ArchInvalid {
		arch = config.HostArchitecture
	}

	if def.params.Kernel != nil {
		ret = append(ret, def.params.Kernel)
	}

	if def.params.InitRamFs != nil {
		ret = append(ret, def.params.InitRamFs)
	}

	for _, directive := range def.params.Directives {
		ret = append(ret, directive)
	}

	return ret, nil
}

// implements common.BuildDefinition.
func (def *BuildVmDefinition) Params() hash.SerializableValue { return def.params }
func (def *BuildVmDefinition) SerializableType() string       { return "BuildVmDefinition" }
func (def *BuildVmDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &BuildVmDefinition{params: params.(BuildVmParameters)}
}

// ToStarlark implements common.BuildDefinition.
func (def *BuildVmDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	return filesystem.NewStarFile(result, def.Tag()), nil
}

// WriteTo implements common.BuildResult.
func (def *BuildVmDefinition) WriteResult(w io.Writer) error {
	if err := def.cmd.Wait(); err != nil {
		return err
	}

	if !def.gotOutput && def.params.OutputFile != "" {
		return fmt.Errorf("VM did not write any output")
	}

	def.server.Shutdown(context.Background())

	def.out.Close()

	return nil
}

func (def *BuildVmDefinition) BuildTemplate(ctx common.BuildContext, hostAddress string) (config.TinyRangeConfig, error) {
	arch, err := config.ArchitectureFromString(def.params.Architecture)
	if err != nil {
		return config.TinyRangeConfig{}, err
	}
	if arch == config.ArchInvalid {
		arch = config.HostArchitecture
	}

	rootArch, err := config.ArchitectureFromString(def.params.RootArchitecture)
	if err != nil {
		return config.TinyRangeConfig{}, err
	}
	if rootArch == config.ArchInvalid {
		rootArch = arch
	}

	builderCfg := config.BuilderConfig{}

	builderCfg.OutputFilename = def.params.OutputFile

	builderCfg.HostAddress = hostAddress

	vmCfg := config.TinyRangeConfig{}

	wd, err := os.Getwd()
	if err != nil {
		return config.TinyRangeConfig{}, err
	}

	var kernelFilename string

	kernelDef := def.params.Kernel
	if kernelDef != nil {
		kernel, err := ctx.BuildChild(kernelDef)
		if err != nil {
			return config.TinyRangeConfig{}, err
		}

		digest, err := ctx.DigestFromFile(kernel)
		if err != nil {
			return config.TinyRangeConfig{}, err
		}

		kernelFilename, err = ctx.FilenameFromDigest(digest)
		if err != nil {
			return config.TinyRangeConfig{}, err
		}
	}

	interaction := def.params.Interaction
	var vmInteraction config.InteractionKind

	if strings.HasPrefix(interaction, "init,") {
		builderCfg.ExecInit = strings.TrimPrefix(interaction, "init,")
		vmInteraction = config.InteractionSerial
	} else {
		vmInteraction = config.InteractionKind(interaction)
	}

	vmCfg.BaseDirectory = wd
	vmCfg.Architecture = arch
	vmCfg.RootArchitecture = rootArch
	vmCfg.KernelFilename = kernelFilename
	vmCfg.CPUCores = def.params.CpuCores
	vmCfg.MemoryMB = def.params.MemoryMB
	vmCfg.Interaction = vmInteraction
	vmCfg.Debug = def.params.Debug

	if def.params.InitRamFs != nil {
		// bypass the default init logic.
		// The user code is expected to call `/init -run-config /builder.json` some how.

		initRamFs, err := ctx.BuildChild(def.params.InitRamFs)
		if err != nil {
			return config.TinyRangeConfig{}, err
		}

		initRamFsDigest, err := ctx.DigestFromFile(initRamFs)
		if err != nil {
			return config.TinyRangeConfig{}, err
		}

		initRamFsFilename, err := ctx.FilenameFromDigest(initRamFsDigest)
		if err != nil {
			return config.TinyRangeConfig{}, err
		}

		vmCfg.InitFilesystemFilename = initRamFsFilename
	}

	initJson := struct {
		SSHCommand []string `json:"ssh_command"`
	}{
		SSHCommand: []string{"/init", "-run-config", "/builder.json"},
	}

	initJsonBytes, err := json.Marshal(&initJson)
	if err != nil {
		return config.TinyRangeConfig{}, err
	}

	var rootFsFragments []config.Fragment

	// Hard code the init file and script.
	rootFsFragments = append(rootFsFragments,
		config.Fragment{Builtin: &config.BuiltinFragment{Name: "init", Architecture: arch, GuestFilename: "/init"}},
		config.Fragment{Builtin: &config.BuiltinFragment{Name: "init.star", GuestFilename: "/init.star"}},
		// Use init.json to set the builder entry point as the SSH command.
		config.Fragment{FileContents: &config.FileContentsFragment{
			Contents:      initJsonBytes,
			GuestFilename: "/init.json",
		}},
	)

	// Launch child builds for each directive.
	for _, directive := range def.params.Directives {
		frags, err := directive.AsFragments(ctx, common.SpecialDirectiveHandlers{
			DefaultInteractive: func(dir common.DirectiveDefaultInteractive) error {
				builderCfg.DefaultInteractive = dir.InteractiveCommand

				return nil
			},
		})
		if err != nil {
			return config.TinyRangeConfig{}, err
		}

		for _, frag := range frags {
			if frag.RunCommand != nil {
				builderCfg.Commands = append(builderCfg.Commands, frag.RunCommand.Command)
			} else if frag.StartServiceCommand != nil {
				builderCfg.ServiceCommands = append(builderCfg.ServiceCommands, frag.StartServiceCommand.Command)
			} else if frag.AddInitScript != nil {
				builderCfg.InitScripts = append(builderCfg.InitScripts, frag.AddInitScript.GuestFilename)
			} else if frag.Environment != nil {
				builderCfg.Environment = append(builderCfg.Environment, frag.Environment.Variables...)
			} else {
				rootFsFragments = append(rootFsFragments, frag)
			}
		}
	}

	buildConfig, err := json.Marshal(&builderCfg)
	if err != nil {
		return config.TinyRangeConfig{}, err
	}

	rootFsFragments = append(rootFsFragments,
		config.Fragment{FileContents: &config.FileContentsFragment{
			Contents:      buildConfig,
			GuestFilename: "/builder.json",
		}},
	)

	vmCfg.Filesystems = make(map[string]config.Filesystem)

	vmCfg.Filesystems["root"] = config.Filesystem{
		Kind:        config.FilesystemKindExt4,
		Fragments:   rootFsFragments,
		StorageSize: def.params.StorageSize,
	}

	return vmCfg, nil
}

// Build implements common.BuildDefinition.
func (def *BuildVmDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	if def.buildTemplateOutput {
		vmCfg, err := def.BuildTemplate(ctx, "")
		if err != nil {
			return nil, err
		}

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

		return nil, ErrTemplateBuilt(configFilename)
	}

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}

	hostAddress := fmt.Sprintf("10.42.0.100:%d", listener.Addr().(*net.TCPAddr).Port)

	vmCfg, err := def.BuildTemplate(ctx, hostAddress)
	if err != nil {
		return nil, err
	}

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

	cmd, err := ctx.RunVMM("qemu", vmCfg)
	if err != nil {
		return nil, err
	}

	def.cmd = cmd

	return def, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *BuildVmDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.ShouldRebuildUserDefinitions() {
		return true, nil
	}

	// TODO(joshua): Check if any of the child directives need to be built.
	return false, nil
}

// Tag implements common.BuildDefinition.
func (def *BuildVmDefinition) Tag() string {
	out := []string{"BuildVm"}

	for _, dir := range def.params.Directives {
		out = append(out, dir.Tag())
	}

	out = append(out, def.params.OutputFile)
	out = append(out, def.params.Interaction)

	if def.params.InitRamFs != nil {
		out = append(out, def.params.InitRamFs.Tag())
	}

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

func NewBuildVmDefinition(
	dir []common.Directive,
	kernel common.BuildDefinition,
	initramfs common.BuildDefinition,
	output string,
	cpuCores int,
	memoryMb int,
	architecture config.CPUArchitecture,
	rootArchitecture config.CPUArchitecture,
	storageSize int,
	interaction string,
	debug bool,
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
			Directives:       dir,
			Kernel:           kernel,
			InitRamFs:        initramfs,
			OutputFile:       output,
			CpuCores:         cpuCores,
			MemoryMB:         memoryMb,
			Architecture:     string(architecture),
			RootArchitecture: string(rootArchitecture),
			StorageSize:      storageSize,
			Interaction:      interaction,
			Debug:            debug,
		},
	}
}
