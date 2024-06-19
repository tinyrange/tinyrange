package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tinyrange/pkg2/v2/common"
	"go.starlark.net/starlark"
)

func runTinyRange(configFilename string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("runTinyRange not implemented")
}

type TinyRangeConfig struct {
	BaseDirectory     string
	RootFsArchives    []string
	Commands          []string
	SupervisorAddress string
	OutputFilename    string
}

type BuildVmDefinition struct {
	// TOOD(joshua): Allow customizing the kernel, hypervisor, and startup script.
	Directives []common.Directive
	OutputFile string

	mux    *http.ServeMux
	server *http.Server
	cmd    *exec.Cmd
	out    io.Closer
}

// WriteTo implements common.BuildResult.
func (def *BuildVmDefinition) WriteTo(w io.Writer) (n int64, err error) {
	def.cmd.Process.Kill()

	def.server.Shutdown(context.Background())

	def.out.Close()

	return 0, nil
}

// Build implements common.BuildDefinition.
func (def *BuildVmDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	config := TinyRangeConfig{}

	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	config.BaseDirectory = wd
	config.OutputFilename = def.OutputFile

	// Launch child builds for each directive.
	for _, directive := range def.Directives {
		switch directive := directive.(type) {
		case *fetchOciImageDefinition:
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

				config.RootFsArchives = append(config.RootFsArchives, filename)
			}
		case common.DirectiveRunCommand:
			config.Commands = append(config.Commands, string(directive))
		default:
			return nil, fmt.Errorf("BuildVmDefinition.Build: directive type %T unhandled", directive)
		}
	}

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}

	config.SupervisorAddress = listener.Addr().String()

	def.mux = http.NewServeMux()

	def.server = &http.Server{
		Handler: def.mux,
	}

	out, err := ctx.CreateOutput()
	if err != nil {
		return nil, err
	}
	def.out = out

	def.mux.HandleFunc("/uploadOutput", func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(out, r.Body)
		if err != nil {
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

	if err := enc.Encode(&config); err != nil {
		out.Close()
		return nil, err
	}

	if err := out.Close(); err != nil {
		return nil, err
	}

	cmd, err := runTinyRange(configFilename)
	if err != nil {
		return nil, err
	}

	def.cmd = cmd

	return def, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *BuildVmDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
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

func NewBuildVmDefinition(dir []common.Directive, output string) *BuildVmDefinition {
	return &BuildVmDefinition{Directives: dir, OutputFile: output}
}
