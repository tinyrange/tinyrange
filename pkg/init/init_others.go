//go:build !linux

package init

import (
	_ "embed"
	"fmt"
	"os"
	"runtime"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
)

//go:embed init
var initExecutable []byte

func GetInitExecutable(arch config.CPUArchitecture) ([]byte, error) {
	if arch == config.ArchInvalid {
		arch = config.HostArchitecture
	}

	if arch.IsNative() {
		if len(initExecutable) == 0 {
			if runtime.GOOS != "linux" {
				return nil, fmt.Errorf("init executable not embedded for %s", runtime.GOOS)
			}

			exec, err := os.Executable()
			if err != nil {
				return nil, fmt.Errorf("could not get executable path: %s", err)
			}

			return os.ReadFile(exec)
		}

		return initExecutable, nil
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
