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

var (
	_ MemoryRegion = &BitmapRegion{}
)

func NewBitmap(size uint64) *BitmapRegion {
	ret := make(BitmapRegion, size/8)

	return &ret
}
