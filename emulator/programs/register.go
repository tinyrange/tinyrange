package programs

import (
	"github.com/tinyrange/tinyrange/emulator/common"
	"github.com/tinyrange/tinyrange/filesystem"
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
