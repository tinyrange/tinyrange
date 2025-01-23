package kernel

import (
	"fmt"
	"os"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
)

var ErrNoOfficialKernel = fmt.Errorf("no official kernel for architecture")

func LoadKernelForArchitecture(arch config.CPUArchitecture) ([]byte, error) {
	kern, err := GetOfficialKernel(arch)
	if err == nil {
		return kern, nil
	}
	if err != ErrNoOfficialKernel {
		return nil, err
	}

	filename, err := common.GetAdjacentFile(fmt.Sprintf("tinyrange_kernel_%s", arch))
	if err != nil {
		return nil, fmt.Errorf("could not get kernel for %s: %s", arch, err)
	}

	kern, err = os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	return kern, nil
}
