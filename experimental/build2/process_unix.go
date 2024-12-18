//go:build unix

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func ProcessRunning(pid int) (bool, error) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, fmt.Errorf("failed to find process: %w", err)
	}

	if err := proc.Signal(unix.Signal(0)); err == nil {
		return true, nil
	}

	return false, nil
}
