package vm

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
)

type VirtualMemory struct {
	pageSize uint32
	// The size of a page is always pageSize.
	// Any pages smaller must be fragmentedRegions and any pages larger must be split into OffsetRegions.
	pages []MemoryRegion
	// Write pages are a mirror of the regular pages that intercept writes and override future reads.
	writePages []*RawRegion
	totalSize  int64

	// stats
	totalMaps         int64
	totalMapRegions   int64
	totalMapFragments int64
}

func (vm *VirtualMemory) PageSize() uint32 { return vm.pageSize }

func (vm *VirtualMemory) mapFragment(region MemoryRegion, offset int64) error {
	vm.totalMapFragments += 1

	// slog.Info("mapFragment", "offset", offset)
	// Get the region index.
	regionIndex := offset / int64(vm.pageSize)
	regionOffset := offset % int64(vm.pageSize)

	existingRegion := vm.pages[uint64(regionIndex)]
	if existingRegion != nil {
		// if a region already exists then check if it's a fragmentedRegion.
		if frag, ok := existingRegion.(*fragmentedRegion); ok {
			// If it's already a fragmentedRegion then map the new part.

			return frag.mapFragment(region, regionOffset)
		} else {
			// Otherwise it's something else.

			// slog.Info("map existingRegion into fragmentRegion",
			// 	"pageSize", vm.pageSize,
			// 	"regionIndex", regionIndex,
			// 	"existingRegion", existingRegion,
			// )

			newFrag := newFragmentRegion(vm.pageSize)

			// Add the existing region.
			if err := newFrag.mapFragment(existingRegion, 0); err != nil {
				return nil
			}

			// Add the new fragment last so it can overwrite the old region.
			if err := newFrag.mapFragment(region, int64(regionOffset)); err != nil {
				return errors.Join(fmt.Errorf("failed to map fragment"), err)
			}

			// slog.Info("", "newFrag", newFrag)

			vm.pages[uint64(regionIndex)] = newFrag

			return nil
		}
	} else {
		newFrag := newFragmentRegion(vm.pageSize)

		if err := newFrag.mapFragment(region, int64(regionOffset)); err != nil {
			return errors.Join(fmt.Errorf("failed to map fragment"), err)
		}

		vm.pages[uint64(regionIndex)] = newFrag

		return nil
	}
}

func (vm *VirtualMemory) mapRegion(region MemoryRegion, offset int64) error {
	vm.totalMapRegions += 1

	if offset%int64(vm.pageSize) != 0 {
		return fmt.Errorf("attempted to use mapRegion to map an unaligned region")
	}

	index := offset / int64(vm.pageSize)

	vm.pages[uint64(index)] = region

	return nil
}

// Size implements MemoryRegion.
func (vm *VirtualMemory) Size() int64 {
	return vm.totalSize
}

// Map a memory region.
func (vm *VirtualMemory) Map(region MemoryRegion, offset int64) error {
	// Unlike physical hardware this virtual memory system can handle non-aligned pages by further subdividing pages.
	// Therefore this map function needs to handle 3 different regions.
	// A potentially oddly sized region at the start.
	// A normal series of page aligned regions in the middle.
	// A potentially oddly sized region at the end.

	vm.totalMaps += 1

	// slog.Info("map", "region", region, "offset", offset)

	// Get the size of the region.
	regionSize := region.Size()

	var regionOffset int64 = 0

	// Check if the offset is aligned.
	if offset%int64(vm.pageSize) != 0 {
		// The offset is unaligned so we need to use a subdivided region.
		if err := vm.mapFragment(region, offset); err != nil {
			return nil
		}

		// Calculate the size of the fragment.
		fragmentSize := int64(vm.pageSize) - (offset % int64(vm.pageSize))

		// Update the offsets.
		regionOffset += fragmentSize
		offset += fragmentSize
	}

	for {
		// If we have no full sized regions left then break.
		if (regionSize - regionOffset) < int64(vm.pageSize) {
			break
		}

		// Map a region.
		if regionOffset == 0 {
			if err := vm.mapRegion(region, offset); err != nil {
				return errors.Join(fmt.Errorf("failed to map region"), err)
			}
		} else {
			if err := vm.mapRegion(NewOffsetRegion(region, regionOffset), offset); err != nil {
				return errors.Join(fmt.Errorf("TODO: %v", err), err)
			}
		}

		// Update the offsets.
		regionOffset += int64(vm.pageSize)
		offset += int64(vm.pageSize)
	}

	// Check for the final oddly sized region.
	if (regionSize - regionOffset) > 0 {
		if err := vm.mapFragment(NewOffsetRegion(region, regionOffset), offset); err != nil {
			return errors.Join(fmt.Errorf("TODO: %v", err), err)
		}
	}

	return nil
}

// Helper function to map a file into a given region.
// The file will be read on demand.
func (vm *VirtualMemory) MapFile(f io.ReaderAt, offset int64, size int64) (*FileRegion, error) {
	ret := &FileRegion{f: f, totalSize: size}

	if err := vm.Map(ret, offset); err != nil {
		return nil, errors.Join(fmt.Errorf("TODO"), err)
	}

	return ret, nil
}

// Copy the data at offset into newRegion and replace the pages there with newRegion.
// This function reinterprets file contents with a new datatype.
func (vm *VirtualMemory) Reinterpret(newRegion MemoryRegion, offset int64) error {
	// Copy the old data to the new structure.
	if _, err := io.Copy(
		io.NewOffsetWriter(newRegion, 0),
		io.NewSectionReader(vm, int64(offset), int64(newRegion.Size())),
	); err != nil {
		return errors.Join(fmt.Errorf("TODO: %v", err), err)
	}

	// Map the region.
	if err := vm.Map(newRegion, offset); err != nil {
		return errors.Join(fmt.Errorf("TODO: %v", err), err)
	}

	return nil
}

func (vm *VirtualMemory) DumpMap(out io.Writer) error {
	// Dump the entire memory map to out.

	var off uint64
	for off = 0; off < uint64(vm.totalSize)/uint64(vm.pageSize); off += 1 {
		region := vm.pages[off]
		if region == nil {
			continue
		}

		switch region := region.(type) {
		case *fragmentedRegion:
			if _, err := fmt.Fprintf(out, "%016X: fragmented\n", off*uint64(vm.pageSize)); err != nil {
				return err
			}
			if err := region.dumpMap(out, off*uint64(vm.pageSize)); err != nil {
				return err
			}
		default:
			regionStr := regionToString(region)

			if _, err := fmt.Fprintf(out, "%016X: %s\n", off*uint64(vm.pageSize), regionStr); err != nil {
				return err
			}
		}
	}

	return nil
}

func (vm *VirtualMemory) getRegion(offset int64, isWrite bool) (MemoryRegion, int64, error) {
	// Find the closest page and start the access there.
	// If the internal access is only partially filled then continue though subsequent pages.
	// This means a read/write larger than a single page skips over connected OffsetRegions.

	// If the offset is more than the total size then return EOF.
	if offset > vm.totalSize {
		return nil, 0, io.EOF
	}

	// Calculate the region index and region offset.
	regionIndex := offset / int64(vm.pageSize)
	regionOffset := offset % int64(vm.pageSize)

	// Get the region.
	region := vm.writePages[uint64(regionIndex)]
	if region != nil {
		// If the writeRegion already exists then just return it.
		return region, regionOffset, nil
	}

	if isWrite {
		newWritePage := make(RawRegion, vm.pageSize)

		existingRegion := vm.pages[uint64(regionIndex)]
		if existingRegion != nil {
			// Populate the region with the existing contents.
			if _, err := existingRegion.ReadAt(newWritePage, 0); err != nil {
				return nil, 0, err
			}
		}

		vm.writePages[uint64(regionIndex)] = &newWritePage

		return vm.writePages[uint64(regionIndex)], regionOffset, nil
	} else {
		region := vm.pages[uint64(regionIndex)]
		if region == nil {
			// A missing region is not a error and just reads zeros.
			return nil, regionOffset, nil
		}

		// Return the region.
		return region, regionOffset, nil
	}
}

// ReadAt implements io.ReaderAt.
func (vm *VirtualMemory) ReadAt(p []byte, off int64) (n int, err error) {
	// Keep looping until we've finished the entire read.
	for {
		// Get the region at offset.
		region, regionOffset, err := vm.getRegion(off, false)
		if err != nil {
			return 0, err
		}

		readSize := 0

		if region != nil {
			// If the region exists then forward the read to the region.
			readSize, err = region.ReadAt(p, regionOffset)
			if err != nil {
				return 0, err
			}
		} else {
			// Otherwise advance the read pointer by the readOffset - pageSize.
			readSize = int(vm.pageSize) - int(regionOffset)

			if readSize > len(p) {
				readSize = len(p)
			}

			if readSize != 0 {
				// Make sure to zero the data.
				copy(p, make([]byte, readSize))
			}
		}

		n += readSize
		p = p[readSize:]
		off += int64(readSize)

		if len(p) == 0 {
			break
		}
	}

	return
}

// WriteAt implements io.WriterAt.
func (vm *VirtualMemory) WriteAt(p []byte, off int64) (n int, err error) {
	// Keep looping until we've finished the entire write.
	for {
		// Get the region at offset.
		region, regionOffset, err := vm.getRegion(off, true)
		if err != nil {
			return 0, err
		}

		writeSize := 0

		if region != nil {
			// If the region exists then forward the write to the region.
			writeSize, err = region.WriteAt(p, regionOffset)
			if err != nil {
				slog.Error("VirtualMemory WriteAt Error", "len", len(p), "off", off, "regionOffset", regionOffset)
				return 0, err
			}
		} else {
			return 0, fmt.Errorf("write to unmapped page at: %X", off)
		}

		n += writeSize
		p = p[writeSize:]
		off += int64(writeSize)

		if len(p) == 0 {
			break
		}
	}

	return
}

func (vm *VirtualMemory) DumpStats() {
	slog.Info("vm stats",
		"totalMaps", vm.totalMaps,
		"totalMapFragments", vm.totalMapFragments,
		"totalMapRegions", vm.totalMapRegions,
		"totalNewOffsetRegionCalls", totalNewOffsetRegionCalls,
	)
}

func (vm *VirtualMemory) DebugOffset(off int64) string {
	region, regionOffset, err := vm.getRegion(off, true)
	if err != nil {
		return fmt.Sprintf("<error: %s>", err)
	}

	switch region := region.(type) {
	case *TruncatedRegion:
		switch childRegion := region.Region.(type) {
		case *PaddedRegion:
			return fmt.Sprintf("<%T:%T:%T %d>", region, region.Region, childRegion.Region, regionOffset)
		default:
			return fmt.Sprintf("<%T:%T %d>", region, region.Region, regionOffset)
		}
	default:
		return fmt.Sprintf("<%T %d>", region, regionOffset)
	}
}

var (
	_ MemoryRegion = &VirtualMemory{}
)

func NewVirtualMemory(totalSize int64, pageSize uint32) *VirtualMemory {
	if totalSize%int64(pageSize) != 0 {
		panic("totalSize%int64(pageSize) != 0")
	}

	return &VirtualMemory{
		pageSize:   pageSize,
		totalSize:  totalSize,
		pages:      make([]MemoryRegion, totalSize/int64(pageSize)),
		writePages: make([]*RawRegion, totalSize/int64(pageSize)),
	}
}
