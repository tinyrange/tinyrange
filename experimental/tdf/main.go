package main

import (
	"os"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/trdf"
)

func appMain() error {
	start := time.Now()
	vmem := vm.NewVirtualMemory(512*1024*1024*1024, 4096)
	log.Info("created virtual memory", "duration", time.Since(start))

	start = time.Now()
	fs, err := ext4.CreateExt4Filesystem(vmem, 0, vmem.Size())
	if err != nil {
		return err
	}
	log.Info("created ext4 filesystem", "duration", time.Since(start))

	_ = fs

	f, err := os.Create("local/disk.img")
	if err != nil {
		return err
	}

	disk, err := trdf.OpenDisk(f, 512*1024*1024*1024)
	if err != nil {
		return err
	}

	start = time.Now()
	if _, err := vmem.WriteSparseTo(disk); err != nil {
		return err
	}
	log.Info("wrote virtual memory to disk", "duration", time.Since(start))

	start = time.Now()
	if err := disk.Flush(); err != nil {
		return err
	}
	log.Info("flushed disk", "duration", time.Since(start))

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}
