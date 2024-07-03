package vm

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"

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
	factory   *VirtualMachineFactory
	kernel    string
	initrd    string
	diskImage string
	nic       *netstack.NetworkInterface
	cmd       *exec.Cmd
	mtx       sync.Mutex
}

func (vm *VirtualMachine) runExecutable(exe *vmmFactoryExecutable, bindOutput bool) error {
	vm.mtx.Lock()

	slog.Info("running hypervisor", "command", exe.command, "args", exe.args)

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
	if name == "kernel" {
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
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (vm *VirtualMachine) AttrNames() []string {
	return []string{
		"kernel",
		"initrd",
		"disk_image",
		"net_send",
		"net_recv",
		"mac_address",
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
	kernel string,
	initrd string,
	diskImage string,
) (*VirtualMachine, error) {
	return &VirtualMachine{
		factory:   factory,
		kernel:    kernel,
		initrd:    initrd,
		diskImage: diskImage,
	}, nil
}

func LoadVirtualMachineFactory(filename string) (*VirtualMachineFactory, error) {
	factory := &VirtualMachineFactory{}

	if err := factory.load(filename); err != nil {
		return nil, err
	}

	return factory, nil
}
