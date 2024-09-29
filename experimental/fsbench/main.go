package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/vm"
)

var (
	size       = flag.Int64("size", 32, "The size of the filesystem in megabytes.")
	pageSize   = flag.Uint("pageSize", 4096, "The size of pages in the underlying virtual memory.")
	count      = flag.Int64("count", 100, "The number of files to add to the filesystem.")
	cpuProfile = flag.String("cpuProfile", "", "The CPU benchmark to write.")
)

var fileContents = vm.RawRegion("Hello, World")

func appMain() error {
	flag.Parse()

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			return err
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}

	start := time.Now()

	_vm := vm.NewVirtualMemory(int64(*pageSize)**size, uint32(*pageSize))

	fs, err := ext4.CreateExt4Filesystem(_vm, 0, _vm.Size())
	if err != nil {
		return err
	}

	var i int64
	for i = 0; i < *count; i++ {
		filename := fmt.Sprintf("/dir%d/hello%d.txt", i%100, i)
		if !fs.Exists(path.Dir(filename)) {
			if err := fs.Mkdir(path.Dir(filename), false); err != nil {
				return err
			}
		}
		if err := fs.CreateFile(filename, fileContents); err != nil {
			return fmt.Errorf("failed to create file %s: %s", filename, err)
		}
	}

	var stats runtime.MemStats

	runtime.ReadMemStats(&stats)

	enc := json.NewEncoder(os.Stdout)

	if err := enc.Encode(&struct {
		Success bool

		Size     int64
		PageSize uint32
		Count    int64

		TotalTime int64
		TotalHeap uint64
	}{
		Success: true,

		Size:     _vm.Size(),
		PageSize: _vm.PageSize(),
		Count:    *count,

		TotalTime: time.Since(start).Nanoseconds(),
		TotalHeap: stats.TotalAlloc,
	}); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		enc := json.NewEncoder(os.Stdout)

		enc.Encode(&struct {
			Success bool
			Message string
		}{
			Success: false,
			Message: err.Error(),
		})

		os.Exit(1)
	}
}
