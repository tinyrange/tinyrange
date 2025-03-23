//go:build linux && !cgo

package init

import (
	"fmt"
	"os"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
)

func GetInitExecutable(arch config.CPUArchitecture) ([]byte, error) {
	if arch == config.ArchInvalid {
		arch = config.HostArchitecture
	}

	if arch.IsNative() {
		exec, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("could not get executable path: %s", err)
		}

		return os.ReadFile(exec)
	} else {
		exe, err := common.GetAdjacentExecutable(fmt.Sprintf("tinyrange_init_%s", arch))
		if err != nil {
			return nil, fmt.Errorf("could not get init executable for %s: %s", arch, err)
		}

		buf, err := os.ReadFile(exe)
		if err != nil {
			return nil, err
		}

		return buf, nil
	}
}
