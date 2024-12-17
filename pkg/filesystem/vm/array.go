package vm

import "fmt"

/*
RegionArray is a conceptually simple type. It is a sequence of tightly packaged MemoryRegions which
are organized next to each other in memory.
*/
type RegionArray[T MemoryRegion] []T

// ReadAt implements MemoryRegion.
func (t *RegionArray[T]) ReadAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(t, off); err != nil {
		return 0, err
	}

	// currentOffset is the number of bytes that have been read from all objects.
	var currentOffset int64 = 0

	for _, child := range *t {
		childLen := child.Size()

		if currentOffset+childLen <= off {
			currentOffset += childLen
			continue
		}

		childOffset := off - currentOffset

		var childN int
		childN, err = child.ReadAt(p, childOffset)
		n += childN
		if err != nil {
			return
		}
		if childN+int(childOffset) > int(childLen) {
			return -1, fmt.Errorf("child was over read: child=%T childN=%d childOffset=%d childLen=%d", child, childN, childOffset, childLen)
		}

		p = p[childN:]
		off += int64(childN)
		currentOffset += childLen

		if len(p) == 0 {
			break
		}
	}

	return
}

// Size implements MemoryRegion.
func (t *RegionArray[T]) Size() int64 {
	var totalSize int64 = 0

	for _, child := range *t {
		totalSize += child.Size()
	}

	return totalSize
}

// WriteAt implements MemoryRegion.
func (t *RegionArray[T]) WriteAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(t, off); err != nil {
		return 0, err
	}

	var currentOffset int64 = 0

	for _, child := range *t {
		childLen := child.Size()

		if currentOffset+childLen <= off {
			currentOffset += childLen
			continue
		}

		childOffset := off - currentOffset

		var childN int
		childN, err = child.WriteAt(p, childOffset)
		n += childN
		if err != nil {
			return
		}

		p = p[childN:]
		off += int64(childN)
		currentOffset += childLen

		if len(p) == 0 {
			break
		}
	}

	return
}

var (
	_ MemoryRegion = &RegionArray[RawRegion]{}
)
