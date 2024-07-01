package main

import (
	"flag"
	"log"
	"os"

	"github.com/tinyrange/tinyrange/filesystem/fat16"
	"github.com/tinyrange/vm"
)

var (
	input = flag.String("input", "", "A input fat16 filesystem")
)

func main() {
	flag.Parse()

	if *input == "" {
		flag.Usage()
		os.Exit(1)
	}

	info, err := os.Stat(*input)
	if err != nil {
		log.Fatal(err)
	}

	in, err := os.Open(*input)
	if err != nil {
		log.Fatal(err)
	}
	defer in.Close()

	_vm := vm.NewVirtualMemory(info.Size(), 4096)

	if _, err := _vm.MapFile(in, 0, info.Size()); err != nil {
		log.Fatal(err)
	}

	fs, err := fat16.MapFat16Filesystem(_vm, 0)
	if err != nil {
		log.Fatal(err)
	}

	_ = fs
}
