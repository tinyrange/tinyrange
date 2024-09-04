package init

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
)

//go:embed init
var INIT_EXECUTABLE []byte

//go:embed init.star
var INIT_SCRIPT []byte

func GetInitExecutable(arch config.CPUArchitecture) ([]byte, error) {
	if arch == config.ArchInvalid {
		arch = config.HostArchitecture
	}

	if arch.IsNative() {
		return INIT_EXECUTABLE, nil
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
