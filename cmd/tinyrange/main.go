package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/tinyrange/tinyrange/pkg/cpio"
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
}

func (vm *VirtualMachine) runExecutable(exe *vmmFactoryExecutable) error {
	cmd := exec.Command(exe.command, exe.args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func (vm *VirtualMachine) Run() error {
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

func (*VirtualMachine) String() string { return "VirtualMachine" }
func (*VirtualMachine) Type() string   { return "VirtualMachine" }
func (*VirtualMachine) Hash() (uint32, error) {
	return 0, fmt.Errorf("VirtualMachine is not hashable")
}
func (*VirtualMachine) Truth() starlark.Bool { return starlark.True }
func (*VirtualMachine) Freeze()              {}

var (
	_ starlark.Value = &VirtualMachine{}
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

func (factory *VirtualMachineFactory) Create() (*VirtualMachine, error) {
	return &VirtualMachine{factory: factory}, nil
}

func LoadVirtualMachineFactory(filename string) (*VirtualMachineFactory, error) {
	factory := &VirtualMachineFactory{}

	if err := factory.load(filename); err != nil {
		return nil, err
	}

	return factory, nil
}

func createRootFilesystem(input string, filename string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	defer f.Close()

	reader, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer reader.Close()

	tarReader := tar.NewReader(reader)

	cpioFs := cpio.New()

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		contents, err := io.ReadAll(tarReader)
		if err != nil {
			return err
		}

		if err := cpioFs.AddFromTar(hdr, contents); err != nil {
			return err
		}
	}

	initScript := "#!/bin/sh\nlogin -f root"

	if err := cpioFs.AddFromTar(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "./init",
		Mode:     int64(fs.ModePerm),
		Size:     int64(len(initScript)),
		ModTime:  time.Unix(0, 0),
	}, []byte(initScript)); err != nil {
		return err
	}

	if err := cpioFs.WriteCpio(filename); err != nil {
		return err
	}

	return nil
}

func tinyRangeMain() error {
	if err := createRootFilesystem(
		"local/alpine-minirootfs-3.20.0-x86_64.tar.gz",
		"local/initramfs.cpio",
	); err != nil {
		return err
	}

	factory, err := LoadVirtualMachineFactory("hv/qemu/qemu.star")
	if err != nil {
		return err
	}

	vm, err := factory.Create()
	if err != nil {
		return err
	}

	if err := vm.Run(); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := tinyRangeMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
