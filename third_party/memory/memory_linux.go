//go:build linux

package memory

import (
	"golang.org/x/sys/unix"
)

func sysTotalMemory() (uint64, error) {
	in := &unix.Sysinfo_t{}
	err := unix.Sysinfo(in)
	if err != nil {
		return 0, err
	}
	// If this is a 32-bit system, then these fields are
	// uint32 instead of uint64.
	// So we always convert to uint64 to match signature.
	return uint64(in.Totalram) * uint64(in.Unit), nil
}
