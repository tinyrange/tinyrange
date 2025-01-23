package kernel

import (
	_ "embed"

	"github.com/tinyrange/tinyrange/pkg/config"
)

//go:embed x86_64/vmlinux
var KERNEL_X86_64 []byte

func GetOfficialKernel(arch config.CPUArchitecture) ([]byte, error) {
	switch arch {
	case config.ArchX8664:
		return KERNEL_X86_64, nil
	default:
		return nil, ErrNoOfficialKernel
	}
}
