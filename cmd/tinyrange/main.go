package main

import (
	"archive/tar"
	_ "embed"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/tinyrange/tinyrange/pkg/cpio"
	"github.com/tinyrange/tinyrange/pkg/vm"
)

//go:embed init.star
var _INIT_SCRIPT []byte

func createRootFilesystem(input string, filename string) error {
	cpioFs := cpio.New()

	init, err := os.ReadFile(input)
	if err != nil {
		return err
	}

	if err := cpioFs.AddFromTar(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "./init",
		Mode:     int64(fs.ModePerm),
		Size:     int64(len(init)),
		ModTime:  time.Unix(0, 0),
	}, init); err != nil {
		return err
	}

	if err := cpioFs.AddFromTar(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "./init.star",
		Mode:     int64(fs.ModePerm),
		Size:     int64(len(_INIT_SCRIPT)),
		ModTime:  time.Unix(0, 0),
	}, _INIT_SCRIPT); err != nil {
		return err
	}

	if err := cpioFs.WriteCpio(filename); err != nil {
		return err
	}

	return nil
}

func tinyRangeMain() error {
	if err := createRootFilesystem(
		"build/init_x86_64",
		"local/initramfs.cpio",
	); err != nil {
		return err
	}

	factory, err := vm.LoadVirtualMachineFactory("hv/qemu/qemu.star")
	if err != nil {
		return err
	}

	vm, err := factory.Create(
		"local/vmlinux_x86_64",
		"local/initramfs.cpio",
	)
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
