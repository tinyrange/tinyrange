package vm

import "io"

type BitmapRegion []byte

// ReadAt implements MemoryRegion.
func (r *BitmapRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(r, off); err != nil {
		return 0, err
	}
	return copy(p, (*r)[off:]), nil
}

// Size implements MemoryRegion.
func (r *BitmapRegion) Size() int64 {
	return int64(len(*r))
}

// WriteAt implements MemoryRegion.
func (r *BitmapRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(r, off); err != nil {
		return 0, err
	}
	return copy((*r)[off:], p), nil
}

func (r *BitmapRegion) Get(i uint64) (bool, error) {
	if i >= uint64(r.Size()*8) {
		return false, io.EOF
	}

	index, pos := i/8, i%8

	return ((*r)[index]>>pos)&0x01 != 0, nil
}

func (r *BitmapRegion) Set(i uint64, value bool) error {
	if i >= uint64(r.Size()*8) {
		return io.EOF
	}

	index, pos := i/8, i%8

	if value {
		(*r)[index] |= 0x01 << pos
	} else {
		(*r)[index] &^= 0x01 << pos
	}

	return nil
}

func (r *BitmapRegion) SetAll(value bool) error {
	for i := range *r {
		if value {
			(*r)[i] = 0xff
		} else {
			(*r)[i] = 0x00
		}
	}
	return nil
}

// SetRange sets a range of bits efficiently using bulk operations
func (r *BitmapRegion) SetRange(start, end uint64, value bool) error {
	if start > end {
		return nil
	}

	maxBits := uint64(r.Size() * 8)
	if start >= maxBits {
		return io.EOF
	}
	if end > maxBits {
		end = maxBits
	}

	// Handle single bit case
	if start == end-1 {
		return r.Set(start, value)
	}

	startByte := start / 8
	endByte := (end - 1) / 8
	startBit := start % 8
	endBit := (end - 1) % 8

	fillByte := byte(0x00)
	if value {
		fillByte = 0xff
	}

	if startByte == endByte {
		// Range is within a single byte
		mask := byte(0)
		for i := startBit; i <= endBit; i++ {
			mask |= 1 << i
		}

		if value {
			(*r)[startByte] |= mask
		} else {
			(*r)[startByte] &^= mask
		}
		return nil
	}

	// Handle partial start byte
	if startBit != 0 {
		mask := byte(0)
		for i := startBit; i < 8; i++ {
			mask |= 1 << i
		}

		if value {
			(*r)[startByte] |= mask
		} else {
			(*r)[startByte] &^= mask
		}
		startByte++
	}

	// Handle partial end byte
	if endBit != 7 {
		mask := byte(0)
		for i := uint64(0); i <= endBit; i++ {
			mask |= 1 << i
		}

		if value {
			(*r)[endByte] |= mask
		} else {
			(*r)[endByte] &^= mask
		}
	} else {
		endByte++
	}

	// Fill complete bytes in between
	for i := startByte; i < endByte; i++ {
		(*r)[i] = fillByte
	}

	return nil
}

var (
	_ MemoryRegion = &BitmapRegion{}
)

func NewBitmap(size uint64) *BitmapRegion {
	ret := make(BitmapRegion, size/8)

	return &ret
}
