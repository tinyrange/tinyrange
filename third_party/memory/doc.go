package memory

import "errors"

var ErrNotSupported = errors.New("not supported")

// TotalMemory returns the total accessible system memory in bytes.
//
// The total accessible memory is installed physical memory size minus reserved
// areas for the kernel and hardware, if such reservations are reported by
// the operating system.
//
// If accessible memory size could not be determined, then 0 is returned.
func TotalMemory() (uint64, error) {
	return sysTotalMemory()
}
