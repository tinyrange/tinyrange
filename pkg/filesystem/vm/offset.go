package vm

// Offset regions have a base and a offset.
type OffsetRegion struct {
	base   MemoryRegion
	offset int64
}

// ReadAt implements MemoryRegion.
func (o *OffsetRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(o, off); err != nil {
		return 0, err
	}

	return o.base.ReadAt(p, off+int64(o.offset))
}

// Size implements MemoryRegion.
func (o *OffsetRegion) Size() int64 {
	return o.base.Size() - o.offset
}

// WriteAt implements MemoryRegion.
func (o *OffsetRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(o, off); err != nil {
		return 0, err
	}

	return o.base.WriteAt(p, off+int64(o.offset))
}

var (
	totalNewOffsetRegionCalls int64 = 0
)

func NewOffsetRegion(base MemoryRegion, offset int64) MemoryRegion {
	if base == nil {
		panic("NewOffsetRegion: base == nil")
	}
	if offset == 0 {
		return base
	} else {
		totalNewOffsetRegionCalls += 1
		if off, ok := base.(*OffsetRegion); ok {
			return &OffsetRegion{base: off.base, offset: off.offset + offset}
		} else {
			return &OffsetRegion{base: base, offset: offset}
		}
	}
}

var (
	_ MemoryRegion = &OffsetRegion{}
)
