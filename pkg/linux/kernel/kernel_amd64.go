package kernel

import (
	_ "embed"
	"fmt"

	"github.com/tinyrange/tinyrange/pkg/config"
)

//go:embed vmlinux_x86_64
var KERNEL_X86_64 []byte

func GetOfficialKernel(arch config.CPUArchitecture) ([]byte, error) {
	switch arch {
	case config.ArchX8664:
		return KERNEL_X86_64, nil
	default:
		return nil, fmt.Errorf("no official kernel for architecture: %s", arch)
	}
}
