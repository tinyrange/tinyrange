package vm

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/netstack"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type vmmFactoryExecutable struct {
	command string
	args    []string
}

func (*vmmFactoryExecutable) String() string { return "Executable" }
func (*vmmFactoryExecutable) Type() string   { return "Executable" }
func (*vmmFactoryExecutable) Hash() (uint32, error) {
	return 0, fmt.Errorf("Executable is not hashable")
}
func (*vmmFactoryExecutable) Truth() starlark.Bool { return starlark.True }
func (*vmmFactoryExecutable) Freeze()              {}

var (
	_ starlark.Value = &vmmFactoryExecutable{}
)

type VirtualMachine struct {
	factory      *VirtualMachineFactory
	cpuCores     int
	memoryMb     int
	architecture config.CPUArchitecture
	kernel       string
	initrd       string
	diskImage    string
	nic          *netstack.NetworkInterface
	cmd          *exec.Cmd
	mtx          sync.Mutex
}

func (vm *VirtualMachine) runExecutable(exe *vmmFactoryExecutable, bindOutput bool) error {
	vm.mtx.Lock()

	slog.Debug("running hypervisor", "command", exe.command, "args", exe.args)

	vm.cmd = exec.Command(exe.command, exe.args...)

	if bindOutput {
		vm.cmd.Stdout = os.Stdout
		vm.cmd.Stderr = os.Stderr
		vm.cmd.Stdin = os.Stdin
	}

	vm.mtx.Unlock()

	return vm.cmd.Run()
}

func (vm *VirtualMachine) Shutdown() error {
	vm.mtx.Lock()
	defer vm.mtx.Unlock()

	if vm.cmd != nil {
		return vm.cmd.Process.Kill()
	}
	return nil
}

func (vm *VirtualMachine) Run(nic *netstack.NetworkInterface, bindOutput bool) error {
	vm.nic = nic

	ret, err := starlark.Call(
		&starlark.Thread{Name: "VirtualMachine"},
		vm.factory.callable,
		starlark.Tuple{vm},
		[]starlark.Tuple{},
	)
	if err != nil {
		return err
	}

	if exec, ok := ret.(*vmmFactoryExecutable); ok {
		return vm.runExecutable(exec, bindOutput)
	} else {
		return fmt.Errorf("expected Executable got %s", ret.Type())
	}
}

// Attr implements starlark.HasAttrs.
func (vm *VirtualMachine) Attr(name string) (starlark.Value, error) {
	if name == "cpu_cores" {
		return starlark.MakeInt(vm.cpuCores), nil
	} else if name == "memory_mb" {
		return starlark.MakeInt(vm.memoryMb), nil
	} else if name == "architecture" {
		return starlark.String(vm.architecture), nil
	} else if name == "kernel" {
		return starlark.String(vm.kernel), nil
	} else if name == "initrd" {
		return starlark.String(vm.initrd), nil
	} else if name == "disk_image" {
		return starlark.String(vm.diskImage), nil
	} else if name == "net_send" {
		return starlark.String(vm.nic.NetSend), nil
	} else if name == "net_recv" {
		return starlark.String(vm.nic.NetRecv), nil
	} else if name == "mac_address" {
		return starlark.String(vm.nic.MacAddress), nil
	} else if name == "accelerate" {
		if vm.Accelerate() {
			return starlark.True, nil
		} else {
			return starlark.False, nil
		}
	} else if name == "verbose" {
		if common.IsVerbose() {
			return starlark.True, nil
		} else {
			return starlark.False, nil
		}
	} else if name == "experimental" {
		return starlark.String(strings.Join(common.GetExperimentalFlags(), ",")), nil
	} else if name == "os" {
		return starlark.String(runtime.GOOS), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (vm *VirtualMachine) AttrNames() []string {
	return []string{
		"cpu_cores",
		"memory_mb",
		"architecture",
		"kernel",
		"initrd",
		"disk_image",
		"net_send",
		"net_recv",
		"mac_address",
		"accelerate",
		"verbose",
		"os",
	}
}

func (*VirtualMachine) String() string { return "VirtualMachine" }
func (*VirtualMachine) Type() string   { return "VirtualMachine" }
func (*VirtualMachine) Hash() (uint32, error) {
	return 0, fmt.Errorf("VirtualMachine is not hashable")
}
func (*VirtualMachine) Truth() starlark.Bool { return starlark.True }
func (*VirtualMachine) Freeze()              {}

var (
	_ starlark.Value    = &VirtualMachine{}
	_ starlark.HasAttrs = &VirtualMachine{}
)

type VirtualMachineFactory struct {
	buildDir string
	callable starlark.Callable
}

func (factory *VirtualMachineFactory) load(filename string) error {
	thread := &starlark.Thread{Name: filename}

	globals := starlark.StringDict{}

	globals["executable"] = starlark.NewBuiltin("executable", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			command        string
			argumentValues starlark.Iterable
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"command", &command,
			"arguments", &argumentValues,
		); err != nil {
			return starlark.None, err
		}

		it := argumentValues.Iterate()
		defer it.Done()

		var (
			arguments []string
			val       starlark.Value
		)
		for it.Next(&val) {
			str, ok := starlark.AsString(val)
			if !ok {
				return starlark.None, fmt.Errorf("expected string got %s", val.Type())
			}

			arguments = append(arguments, str)
		}

		return &vmmFactoryExecutable{
			command: command,
			args:    arguments,
		}, nil
	})

	globals["error"] = starlark.NewBuiltin("error", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			message string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"message", &message,
		); err != nil {
			return starlark.None, err
		}

		return starlark.None, errors.New(message)
	})

	globals["find_command"] = starlark.NewBuiltin("find_command", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			command string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"command", &command,
		); err != nil {
			return starlark.None, err
		}

		exec, err := exec.LookPath(command)
		if err == nil {
			return starlark.String(exec), nil
		} else {
			return starlark.None, nil
		}
	})

	globals["find_local"] = starlark.NewBuiltin("find_local", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			file string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"file", &file,
		); err != nil {
			return starlark.None, err
		}

		path := filepath.Join(filepath.Dir(filename), file)

		if ok, _ := common.Exists(path); ok {
			return starlark.String(path), nil
		} else {
			return starlark.None, nil
		}
	})

	globals["path_exists"] = starlark.NewBuiltin("path_exists", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			path string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"path", &path,
		); err != nil {
			return starlark.None, err
		}

		if ok, _ := common.Exists(path); ok {
			return starlark.True, nil
		} else {
			return starlark.False, nil
		}
	})

	globals["write_file_to_build"] = starlark.NewBuiltin("write_file_to_build", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			contents string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"contents", &contents,
		); err != nil {
			return starlark.None, err
		}

		contents = strings.ReplaceAll(contents, "\n", "")
		contents = strings.ReplaceAll(contents, " ", "")

		dec := base64.NewDecoder(base64.StdEncoding, bytes.NewReader([]byte(contents)))

		contentsBytes, err := io.ReadAll(dec)
		if err != nil {
			return starlark.None, err
		}

		hash := hash.GetSha256Hash(contentsBytes)

		path := filepath.Join(factory.buildDir, hash+".bin")

		if ok, _ := common.Exists(path); !ok {
			if err := os.WriteFile(path, contentsBytes, os.ModePerm); err != nil {
				return starlark.None, err
			}
		}

		return starlark.String(path), nil
	})

	declared, err := starlark.ExecFileOptions(&syntax.FileOptions{
		Set:             true,
		While:           true,
		TopLevelControl: true,
	}, thread, filename, nil, globals)
	if err != nil {
		return err
	}

	mainFunc, ok := declared["main"]
	if !ok {
		return fmt.Errorf("could not find main function in VirtualMachineFactory")
	}

	callable, ok := mainFunc.(starlark.Callable)
	if !ok {
		return fmt.Errorf("expected Callable got %s", mainFunc.Type())
	}

	factory.callable = callable

	return nil
}

func (factory *VirtualMachineFactory) Create(
	cpuCores int,
	memoryMb int,
	architecture config.CPUArchitecture,
	kernel string,
	initrd string,
	diskImage string,
) (*VirtualMachine, error) {
	return &VirtualMachine{
		factory:      factory,
		cpuCores:     cpuCores,
		memoryMb:     memoryMb,
		architecture: architecture,
		kernel:       kernel,
		initrd:       initrd,
		diskImage:    diskImage,
	}, nil
}

func LoadVirtualMachineFactory(buildDir string, filename string) (*VirtualMachineFactory, error) {
	factory := &VirtualMachineFactory{
		buildDir: buildDir,
	}

	if err := factory.load(filename); err != nil {
		return nil, err
	}

	return factory, nil
}
