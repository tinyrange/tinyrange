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

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/feature"
	"github.com/tinyrange/tinyrange/pkg/filesystem/star"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&buildVmDefinition{})
}

type buildVmDefinition struct {
	params BuildVmParameters

	buildTemplateOutput bool

	mux       *http.ServeMux
	server    *http.Server
	cmd       *exec.Cmd
	out       io.WriteCloser
	gotOutput bool
}

// AsFragments implements common.Directive.
func (def *buildVmDefinition) AsFragments(ctx common.BuildContext, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	if def.params.OutputFile == "/init/changed.archive" {
		art, err := ctx.BuildChild(def)
		if err != nil {
			return nil, err
		}

		res, err := art.Default()
		if err != nil {
			return nil, err
		}

		filename, err := ctx.HostFilenameFromFile(res)
		if err != nil {
			return nil, err
		}

		return []config.Fragment{
			{Archive: &config.ArchiveFragment{HostFilename: filename}},
		}, nil
	} else {
		return nil, fmt.Errorf("unknown output file: %s", def.params.OutputFile)
	}
}

// Dependencies implements common.BuildDefinition.
func (def *buildVmDefinition) Dependencies() ([]common.BuildDefinition, error) {
	var deps []common.BuildDefinition

	if def.params.Kernel != nil {
		deps = append(deps, def.params.Kernel)
	}
	if def.params.InitRamFs != nil {
		deps = append(deps, def.params.InitRamFs)
	}

	for _, dir := range def.params.Directives {
		if dir == def {
			return nil, fmt.Errorf("circular buildVM dependency: %+v", def)
		}

		deps, err := dir.Dependencies()
		if err != nil {
			return nil, err
		}

		for _, dep := range deps {
			if dep == def {
				return nil, fmt.Errorf("circular buildVM dependency: %+v", def)
			}
			deps = append(deps, dep)
		}
	}

	return deps, nil
}

func (def *buildVmDefinition) SetBuildTemplateMode() {
	def.buildTemplateOutput = true
}

// implements common.BuildDefinition.
func (def *buildVmDefinition) Params() hash.SerializableValue { return def.params }
func (def *buildVmDefinition) SerializableType() string       { return "BuildVmDefinition" }
func (def *buildVmDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &buildVmDefinition{params: params.(BuildVmParameters)}
}

// ToStarlark implements common.BuildDefinition.
func (def *buildVmDefinition) ToStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	return star.NewStarFile(result, artifact.DefinitionHash().String()), nil
}

// WriteTo implements common.BuildResult.
func (def *buildVmDefinition) WriteResult(w io.Writer) error {
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

func (def *buildVmDefinition) BuildTemplate(ctx common.BuildContext, hostAddress string) (config.TinyRangeConfig, error) {
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

		kernelFile, err := kernel.Default()
		if err != nil {
			return config.TinyRangeConfig{}, err
		}

		kernelFilename, err = ctx.HostFilenameFromFile(kernelFile)
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

		initRamFsFile, err := initRamFs.Default()
		if err != nil {
			return config.TinyRangeConfig{}, err
		}

		initRamFsFilename, err := ctx.HostFilenameFromFile(initRamFsFile)
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
			} else if frag.RunStarlarkScript != nil {
				builderCfg.StarlarkScripts = append(builderCfg.StarlarkScripts, frag.RunStarlarkScript.Script)
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
func (def *buildVmDefinition) Build(ctx common.BuildContext) error {
	if def.buildTemplateOutput {
		vmCfg, err := def.BuildTemplate(ctx, "")
		if err != nil {
			return err
		}

		_, err = ctx.RunVMM("", vmCfg)
		if err != nil {
			return err
		} else {
			return fmt.Errorf("expected error")
		}
	}

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return err
	}

	hostAddress := fmt.Sprintf("10.42.0.100:%d", listener.Addr().(*net.TCPAddr).Port)

	vmCfg, err := def.BuildTemplate(ctx, hostAddress)
	if err != nil {
		return err
	}

	def.mux = http.NewServeMux()

	def.server = &http.Server{
		Handler: def.mux,
	}

	out, err := ctx.CreateDefault()
	if err != nil {
		return err
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

	if feature.HasFeature(feature.FeatureVz) {
		cmd, err := ctx.RunVMM("vz", vmCfg)
		if err != nil {
			return err
		}

		def.cmd = cmd
	} else {
		cmd, err := ctx.RunVMM("qemu", vmCfg)
		if err != nil {
			return err
		}

		def.cmd = cmd
	}

	return def.WriteResult(nil)
}

// NeedsBuild implements common.BuildDefinition.
func (def *buildVmDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	if ctx.ShouldRebuildUserDefinitions() {
		return true, nil
	}

	// TODO(joshua): Check if any of the child directives need to be built.
	return false, nil
}

func (def *buildVmDefinition) String() string {
	var parts []string

	for _, dir := range def.params.Directives {
		parts = append(parts, dir.SerializableType())
	}

	return fmt.Sprintf("BuildVmDefinition(%s)", strings.Join(parts, ", "))
}

func (*buildVmDefinition) Type() string { return "BuildVmDefinition" }
func (*buildVmDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("BuildVmDefinition is not hashable")
}
func (*buildVmDefinition) Truth() starlark.Bool { return starlark.True }
func (*buildVmDefinition) Freeze()              {}

var (
	_ starlark.Value         = &buildVmDefinition{}
	_ common.BuildDefinition = &buildVmDefinition{}
	_ common.BuildResult     = &buildVmDefinition{}
	_ common.Directive       = &buildVmDefinition{}
)

func newBuildVmDefinition(
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
) BuildVmDefinition {
	if storageSize == 0 {
		storageSize = 1024
	}
	if cpuCores == 0 {
		cpuCores = 1
	}
	if memoryMb == 0 {
		memoryMb = 1024
	}
	return &buildVmDefinition{
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
