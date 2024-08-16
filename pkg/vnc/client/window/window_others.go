//go:build !linux

package window

import (
	"fmt"
	"runtime"
)

func New() (Window, error) {
	return nil, fmt.Errorf("not implemented on %s", runtime.GOOS)
}
