package vm

type TruncatedRegion struct {
	Region  MemoryRegion
	MaxSize int64
}

// ReadAt implements MemoryRegion.
func (t *TruncatedRegion) ReadAt(p []byte, off int64) (n int, err error) {
	return t.Region.ReadAt(p, off)
}

// Size implements MemoryRegion.
func (t *TruncatedRegion) Size() int64 {
	return t.MaxSize
}

// WriteAt implements MemoryRegion.
func (t *TruncatedRegion) WriteAt(p []byte, off int64) (n int, err error) {
	return t.Region.WriteAt(p, off)
}

var (
	_ MemoryRegion = &TruncatedRegion{}
)

func NewTruncatedRegion(region MemoryRegion, size int64) MemoryRegion {
	if region.Size() < size {
		return region
	}

	return &TruncatedRegion{Region: region, MaxSize: size}
}
