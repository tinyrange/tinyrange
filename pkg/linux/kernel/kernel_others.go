//go:build !amd64 && !arm64

package kernel

import (
	"fmt"

	"github.com/tinyrange/tinyrange/pkg/config"
)

func GetOfficialKernel(arch config.CPUArchitecture) ([]byte, error) {
	return nil, fmt.Errorf("no official kernel for architecture: %s", arch)
}
