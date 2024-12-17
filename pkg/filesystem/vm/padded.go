package vm

type PaddedRegion struct {
	Region     MemoryRegion
	RegionSize int64
}

// ReadAt implements MemoryRegion.
func (r *PaddedRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(r, off); err != nil {
		return 0, err
	}

	// If the offset is less than the size of the region then perform the read.
	if off < r.Region.Size() {
		// Perform read
		n, err = r.Region.ReadAt(p, off)
		if err != nil {
			return -1, err
		}
	}

	// If the read is smaller than the requested size, pad with zeros
	padSize := min(len(p), int(r.RegionSize-off))
	if n < padSize {
		// log.Info("pad", "n", n, "padSize", padSize)
		for i := n; i < padSize; i++ {
			p[i] = 0
		}
	}

	return padSize, err
}

// Size implements MemoryRegion.
func (r *PaddedRegion) Size() int64 { return r.RegionSize }

// WriteAt implements MemoryRegion.
func (r *PaddedRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(r, off); err != nil {
		return 0, err
	}

	if off > r.Region.Size() {
		return int(min(r.RegionSize-r.Region.Size(), int64(len(p)))), nil
	}

	childN, err := r.Region.WriteAt(p, off)
	n += childN
	if err != nil {
		return
	}

	n += (int(min(r.RegionSize-r.Region.Size(), int64(len(p)))) - n)

	return
}

var (
	_ MemoryRegion = &PaddedRegion{}
)

func NewPaddedRegion(region MemoryRegion, size int64) *PaddedRegion {
	return &PaddedRegion{
		Region:     region,
		RegionSize: size,
	}
}
