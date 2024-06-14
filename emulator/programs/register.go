package programs

import (
	"github.com/tinyrange/pkg2/v2/emulator/common"
	"github.com/tinyrange/pkg2/v2/filesystem"
)

func AddShellUtilities(emu common.Emulator) error {
	for _, err := range []error{
		emu.AddFile("/bin/sh", &Shell{File: filesystem.NewMemoryFile()}),
	} {
		if err != nil {
			return err
		}
	}

	return nil
}
