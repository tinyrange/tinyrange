package vm

import (
	"fmt"
)

type RawRegion []byte

func (t RawRegion) String() string {
	return fmt.Sprintf("<%d>", len(t))
}

// ReadAt implements MemoryRegion.
func (t RawRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(t, off); err != nil {
		return 0, err
	}
	return copy(p, t[off:]), nil
}

// Size implements MemoryRegion.
func (t RawRegion) Size() int64 {
	return int64(len(t))
}

// WriteAt implements MemoryRegion.
func (t RawRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(t, off); err != nil {
		return 0, err
	}
	return copy(t[off:], p), nil
}

var (
	_ MemoryRegion = &RawRegion{}
)
