package vm

import (
	"fmt"
	"sort"
	"sync"
)

type GenericRegionArray interface {
	MemoryRegion
	GetRegions() []MemoryRegion
}

/*
RegionArray is a conceptually simple type. It is a sequence of tightly packaged MemoryRegions which
are organized next to each other in memory.
*/
type RegionArray[T MemoryRegion] struct {
	regions           []T
	cumulativeOffsets []int64      // Cached cumulative offsets for fast binary search
	totalSize         int64        // Cached total size
	offsetsValid      bool         // Whether the cached offsets are valid
	mutex             sync.RWMutex // Protects the cache
}

// GetRegions returns the regions in the array
func (t *RegionArray[T]) GetRegions() []MemoryRegion {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	regions := make([]MemoryRegion, len(t.regions))
	for i, region := range t.regions {
		regions[i] = region
	}
	return regions
}

// rebuildCache rebuilds the cumulative offset cache
func (t *RegionArray[T]) rebuildCache() {
	if t.offsetsValid {
		return
	}

	if len(t.regions) == 0 {
		t.cumulativeOffsets = nil
		t.totalSize = 0
		t.offsetsValid = true
		return
	}

	t.cumulativeOffsets = make([]int64, len(t.regions))
	var offset int64 = 0

	for i, region := range t.regions {
		t.cumulativeOffsets[i] = offset
		offset += region.Size()
	}

	t.totalSize = offset
	t.offsetsValid = true
}

// ensureCache ensures the cache is valid (thread-safe)
func (t *RegionArray[T]) ensureCache() {
	t.mutex.RLock()
	if t.offsetsValid {
		t.mutex.RUnlock()
		return
	}
	t.mutex.RUnlock()

	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.rebuildCache()
}

// findRegionIndex uses binary search to find the region containing the given offset
func (t *RegionArray[T]) findRegionIndex(offset int64) int {
	t.ensureCache()

	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if len(t.cumulativeOffsets) == 0 {
		return -1
	}

	// Binary search for the last region that starts at or before offset
	idx := sort.Search(len(t.cumulativeOffsets), func(i int) bool {
		return t.cumulativeOffsets[i] > offset
	}) - 1

	if idx < 0 {
		return 0
	}

	return idx
}

// ReadAt implements MemoryRegion.
func (t *RegionArray[T]) ReadAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(t, off); err != nil {
		return 0, err
	}

	// Use binary search to find the starting region
	startIdx := t.findRegionIndex(off)
	if startIdx < 0 || startIdx >= len(t.regions) {
		return 0, nil
	}

	// Get the cumulative offset for the starting region
	t.mutex.RLock()
	currentOffset := t.cumulativeOffsets[startIdx]
	t.mutex.RUnlock()

	// Process regions starting from the found index
	for i := startIdx; i < len(t.regions) && len(p) > 0; i++ {
		child := t.regions[i]
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
	}

	return
}

// Size implements MemoryRegion.
func (t *RegionArray[T]) Size() int64 {
	t.ensureCache()

	t.mutex.RLock()
	defer t.mutex.RUnlock()

	return t.totalSize
}

// WriteAt implements MemoryRegion.
func (t *RegionArray[T]) WriteAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(t, off); err != nil {
		return 0, err
	}

	// Use binary search to find the starting region
	startIdx := t.findRegionIndex(off)
	if startIdx < 0 || startIdx >= len(t.regions) {
		return 0, nil
	}

	// Get the cumulative offset for the starting region
	t.mutex.RLock()
	currentOffset := t.cumulativeOffsets[startIdx]
	t.mutex.RUnlock()

	// Process regions starting from the found index
	for i := startIdx; i < len(t.regions) && len(p) > 0; i++ {
		child := t.regions[i]
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
	}

	return
}

// Append adds a region to the array and invalidates the cache
func (t *RegionArray[T]) Append(region T) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.regions = append(t.regions, region)
	t.offsetsValid = false
}

// Set replaces the region at the given index and invalidates the cache
func (t *RegionArray[T]) Set(index int, region T) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if index >= 0 && index < len(t.regions) {
		t.regions[index] = region
		t.offsetsValid = false
	}
}

// Len returns the number of regions in the array
func (t *RegionArray[T]) Len() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	return len(t.regions)
}

// Get returns the region at the given index
func (t *RegionArray[T]) Get(index int) T {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if index >= 0 && index < len(t.regions) {
		return t.regions[index]
	}

	var zero T
	return zero
}

// Range calls the provided function for each region in the array
func (t *RegionArray[T]) Range(fn func(int, T) bool) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	for i, region := range t.regions {
		if !fn(i, region) {
			break
		}
	}
}

// NewRegionArray creates a new optimized RegionArray
func NewRegionArray[T MemoryRegion](initialRegions ...T) *RegionArray[T] {
	arr := &RegionArray[T]{
		regions:      make([]T, len(initialRegions)),
		offsetsValid: false,
	}
	copy(arr.regions, initialRegions)
	return arr
}

var (
	_ MemoryRegion = &RegionArray[RawRegion]{}
)
