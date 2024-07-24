package programs

import (
	fsCommon "github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/emulator/common"
)

func AddShellUtilities(emu common.Emulator) error {
	for _, err := range []error{
		emu.AddFile("/bin/sh", &Shell{File: fsCommon.NewMemoryFile(fsCommon.TypeRegular)}),
	} {
		if err != nil {
			return err
		}
	}

	return nil
}
