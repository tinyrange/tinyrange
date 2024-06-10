package vm

import (
	"fmt"
	"os"
	"os/exec"

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
	factory *VirtualMachineFactory
	kernel  string
	initrd  string
	ns      *netstack.NetStack
	nic     *netstack.NetworkInterface
}

func (vm *VirtualMachine) runExecutable(exe *vmmFactoryExecutable) error {
	cmd := exec.Command(exe.command, exe.args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func (vm *VirtualMachine) Run() error {
	nic, err := vm.ns.AttachNetworkInterface()
	if err != nil {
		return err
	}

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
		return vm.runExecutable(exec)
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
	return []string{"kernel", "initrd"}
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
	ns *netstack.NetStack,
) (*VirtualMachine, error) {
	return &VirtualMachine{factory: factory, ns: ns, kernel: kernel, initrd: initrd}, nil
}

func LoadVirtualMachineFactory(filename string) (*VirtualMachineFactory, error) {
	factory := &VirtualMachineFactory{}

	if err := factory.load(filename); err != nil {
		return nil, err
	}

	return factory, nil
}
