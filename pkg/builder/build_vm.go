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

var OFFICIAL_KERNEL_URL_X86_64 = "https://github.com/tinyrange/linux_build/releases/download/linux_x86_6.6.7/vmlinux_x86_64"
var OFFICIAL_KERNEL_URL_AARCH64 = "https://github.com/tinyrange/linux_build/releases/download/linux_arm64_6.6.7/vmlinux_arm64"

func runTinyRange(exe string, configFilename string) (*exec.Cmd, error) {
	cmd := exec.Command(exe, "run", configFilename)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.Debug("executing tinyrange", "args", cmd.Args)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

type BuildVmDefinition struct {
	params BuildVmParameters

	mux       *http.ServeMux
	server    *http.Server
	cmd       *exec.Cmd
	out       io.WriteCloser
	gotOutput bool
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

	kernelDef := def.params.Kernel
	if kernelDef == nil {
		if arch == config.ArchX8664 {
			kernelDef = NewFetchHttpBuildDefinition(OFFICIAL_KERNEL_URL_X86_64, 0, nil)
		} else if arch == config.ArchARM64 {
			kernelDef = NewFetchHttpBuildDefinition(OFFICIAL_KERNEL_URL_AARCH64, 0, nil)
		} else {
			return nil, fmt.Errorf("no kernel specified and no official kernel available for %s", arch)
		}
	}

	ret = append(ret, kernelDef)

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

// Build implements common.BuildDefinition.
func (def *BuildVmDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	arch, err := config.ArchitectureFromString(def.params.Architecture)
	if err != nil {
		return nil, err
	}
	if arch == config.ArchInvalid {
		arch = config.HostArchitecture
	}

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
		if arch == config.ArchX8664 {
			kernelDef = NewFetchHttpBuildDefinition(OFFICIAL_KERNEL_URL_X86_64, 0, nil)
		} else if arch == config.ArchARM64 {
			kernelDef = NewFetchHttpBuildDefinition(OFFICIAL_KERNEL_URL_AARCH64, 0, nil)
		} else {
			return nil, fmt.Errorf("no kernel specified and no official kernel available for %s", arch)
		}
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
		return nil, fmt.Errorf("could not find default hypervisor tinyrange_qemu.star: %s", hvScript)
	}

	interaction := def.params.Interaction

	if strings.HasPrefix(interaction, "init,") {
		builderCfg.ExecInit = strings.TrimPrefix(interaction, "init,")
		interaction = "serial"
	}

	vmCfg.BaseDirectory = wd
	vmCfg.Architecture = arch
	vmCfg.HypervisorScript = hvScript
	vmCfg.KernelFilename = kernelFilename
	vmCfg.CPUCores = def.params.CpuCores
	vmCfg.MemoryMB = def.params.MemoryMB
	vmCfg.StorageSize = def.params.StorageSize
	vmCfg.Interaction = interaction
	vmCfg.Debug = def.params.Debug

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
		config.Fragment{Builtin: &config.BuiltinFragment{Name: "init", Architecture: arch, GuestFilename: "/init"}},
		config.Fragment{Builtin: &config.BuiltinFragment{Name: "init.star", GuestFilename: "/init.star"}},
		// Use init.json to set the builder entry point as the SSH command.
		config.Fragment{FileContents: &config.FileContentsFragment{
			Contents:      []byte("{\"ssh_command\": [\"/init\", \"-run-config\", \"/builder.json\"]}"),
			GuestFilename: "/init.json",
		}},
	)

	// Launch child builds for each directive.
	for _, directive := range def.params.Directives {
		frags, err := directive.AsFragments(ctx)
		if err != nil {
			return nil, err
		}

		for _, frag := range frags {
			if frag.RunCommand != nil {
				builderCfg.Commands = append(builderCfg.Commands, frag.RunCommand.Command)
			} else if frag.Environment != nil {
				builderCfg.Environment = append(builderCfg.Environment, frag.Environment.Variables...)
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

	exe, err := os.Executable()
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
			Directives:   dir,
			Kernel:       kernel,
			InitRamFs:    initramfs,
			OutputFile:   output,
			CpuCores:     cpuCores,
			MemoryMB:     memoryMb,
			Architecture: string(architecture),
			StorageSize:  storageSize,
			Interaction:  interaction,
			Debug:        debug,
		},
	}
}
