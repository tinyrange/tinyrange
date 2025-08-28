//go:build windows && amd64

package main

import (
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"github.com/tinyrange/tinyrange/pkg/log"
)

func appMain() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	part, err := WHvCreatePartition()
	if err != nil {
		return fmt.Errorf("failed to create partition: %w", err)
	}
	defer WHvDeletePartition(part)

	var processorCount uint32 = 1
	if err := WHvSetPartitionProperty(
		part,
		WHvPartitionPropertyCodeProcessorCount,
		unsafe.Pointer(&processorCount),
		uint32(unsafe.Sizeof(processorCount)),
	); err != nil {
		return fmt.Errorf("failed to set partition property: %w", err)
	}

	if err := WHvSetupPartition(part); err != nil {
		return fmt.Errorf("failed to setup partition: %w", err)
	}

	if err := WHvCreateVirtualProcessor(part, 0, 0); err != nil {
		return fmt.Errorf("failed to create virtual processor: %w", err)
	}
	defer WHvDeleteVirtualProcessor(part, 0)

	ret := make([]byte, 0x1000)

	if err := WHvRunVirtualProcessor(part, 0, unsafe.Pointer(&ret[0]), uint32(len(ret))); err != nil {
		return fmt.Errorf("failed to run virtual processor: %w", err)
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Default().Error("fatal", "error", err)
		os.Exit(1)
	}
}
