package programs

import (
	"github.com/tinyrange/tinyrange/filesystem"
	"github.com/tinyrange/tinyrange/pkg/emulator/common"
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
