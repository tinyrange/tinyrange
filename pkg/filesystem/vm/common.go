package vm

import (
	"fmt"
	"io"
)

type MemoryRegion interface {
	// Offsets are given relative to the start of the region.
	// Offsets will be less than Size() and non-negative.
	io.ReaderAt
	io.WriterAt
	Size() int64
}

func boundsCheck(m MemoryRegion, off int64) error {
	if off < 0 {
		return fmt.Errorf("off < 0")
	}

	if off >= m.Size() {
		return io.EOF
	}

	return nil
}
