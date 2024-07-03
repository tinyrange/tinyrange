package programs

import (
	"github.com/tinyrange/tinyrange/pkg/emulator/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

func AddShellUtilities(emu common.Emulator) error {
	for _, err := range []error{
		emu.AddFile("/bin/sh", &Shell{File: filesystem.NewMemoryFile(filesystem.TypeRegular)}),
	} {
		if err != nil {
			return err
		}
	}

	return nil
}
