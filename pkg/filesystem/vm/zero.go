package vm

import (
	"fmt"
)

type ZeroRegion int64

func (t ZeroRegion) String() string {
	return fmt.Sprintf("<zero %d>", int64(t))
}

// ReadAt implements MemoryRegion.
func (t ZeroRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(t, off); err != nil {
		return 0, err
	}
	return copy(p, make([]byte, len(p))), nil
}

// Size implements MemoryRegion.
func (t ZeroRegion) Size() int64 {
	return int64(t)
}

// WriteAt implements MemoryRegion.
func (t ZeroRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(t, off); err != nil {
		return 0, err
	}
	return len(p), nil
}

var (
	_ MemoryRegion = ZeroRegion(0)
)
