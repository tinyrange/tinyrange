//go:build !linux

package common

import (
	"fmt"
	"runtime"
)

func EscalateToRoot() error {
	return fmt.Errorf("common.EscalateToRoot not implemented for %s", runtime.GOOS)
}

func MountTempFilesystem(path string) error {
	return fmt.Errorf("common.MountTempFilesystem not implemented for %s", runtime.GOOS)
}
