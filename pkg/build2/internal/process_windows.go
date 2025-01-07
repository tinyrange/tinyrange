//go:build windows

package internal

import (
	"fmt"
	"os"
)

func processRunning(pid int) (bool, error) {
	_, err := os.FindProcess(pid)
	if err != nil {
		return false, fmt.Errorf("failed to find process: %w", err)
	}

	return true, nil
}
