//go:build !linux

package builder

import (
	"fmt"
	"runtime"
)

func (frags *FragmentsBuilderResult) ExtractAndRunScripts(target string) error {
	return fmt.Errorf("not supported on %s", runtime.GOOS)
}
