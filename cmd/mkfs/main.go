package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
)

var (
	size   = flag.Int("size", 64, "the size of the filesystem in megabytes")
	output = flag.String("output", "", "the file to write the filesystem to")
)

func mkfsMain() error {
	flag.Parse()

	if *output == "" {
		flag.Usage()
		return fmt.Errorf("output must be specified")
	}

	fsSize := int64(*size * 1024 * 1024)

	vm := vm.NewVirtualMemory(fsSize, 4096)

	fs, err := ext4.CreateExt4Filesystem(vm, 0, fsSize)
	if err != nil {
		return err
	}

	for _, in := range flag.Args() {
		if err := filesystem.ExtractArchiveTo(in, fs); err != nil {
			return err
		}
	}

	out, err := os.Create(*output)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, io.NewSectionReader(vm, 0, fsSize)); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := mkfsMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
