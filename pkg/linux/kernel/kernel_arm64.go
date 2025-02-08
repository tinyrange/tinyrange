package kernel

import (
	_ "embed"

	"github.com/tinyrange/tinyrange/pkg/config"
)

//go:embed aarch64/vmlinux
var KERNEL_AARCH64 []byte

func GetOfficialKernel(arch config.CPUArchitecture) ([]byte, error) {
	switch arch {
	case config.ArchARM64:
		return KERNEL_AARCH64, nil
	default:
		return nil, ErrNoOfficialKernel
	}
}
