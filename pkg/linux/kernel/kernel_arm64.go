package kernel

import (
	_ "embed"
	"fmt"

	"github.com/tinyrange/tinyrange/pkg/config"
)

//go:embed vmlinux_arm64
var KERNEL_ARM64 []byte

func GetOfficialKernel(arch config.CPUArchitecture) ([]byte, error) {
	switch arch {
	case config.ArchARM64:
		return KERNEL_ARM64, nil
	default:
		return nil, fmt.Errorf("no official kernel for architecture: %s", arch)
	}
}
