package vm

import (
	"fmt"
	"io"
	"log/slog"
)

// regionFragment is effectively an extent.
// Reads from beyond size and before offset are illegal for the underlying region.
type regionFragment struct {
	region MemoryRegion
	offset int64
	size   int64
}

func (f regionFragment) String() string {
	return fmt.Sprintf("%08X-%08X", f.start(), f.end())
}

func (f regionFragment) start() int64 { return f.offset }
func (f regionFragment) end() int64   { return f.offset + f.size }

// Return a new region starting at off.
func (f regionFragment) offsetAt(off int64) *regionFragment {
	if off > f.size {
		panic("offsetAt: off > f.size")
	}

	return &regionFragment{
		region: NewOffsetRegion(f.region, off),
		offset: f.offset + off,
		size:   f.size - off,
	}
}

// Return a new region ending at off.
func (f regionFragment) cutAt(off int64) *regionFragment {
	if off > f.size {
		panic("cutAt: off > f.size")
	}

	return &regionFragment{
		region: f.region,
		offset: f.offset,
		size:   off,
	}
}

func remapRegion(old *regionFragment, new *regionFragment) []*regionFragment {
	// This function assumes that old and new can't overlap at the start.
	// That would have been picked up and resolved by mapFragment.
	oldStart := old.start()
	oldEnd := old.end()

	newStart := new.start()
	newEnd := new.end()

	// If old and end don't overlap at all then just return old.
	if oldEnd <= newStart || newEnd <= oldStart {
		// slog.Info("case 1", "old", old, "new", new)
		return []*regionFragment{old}
	}

	// If old and new completely overlap or new overwrites old then just return new.
	if newStart == oldStart && newEnd >= oldEnd {
		// slog.Info("case 2", "old", old, "new", new)
		return []*regionFragment{new}
	}

	// If the start of old and new perfectly overlap but the end doesn't overlap
	// then return new, old[offset]
	if newStart == oldStart && newEnd < oldEnd {
		// Find the overlap at the end.
		endOverlap := new.size

		// slog.Info("case 3", "old", old, "new", new, "endOverlap", endOverlap)

		return []*regionFragment{
			new,
			old.offsetAt(endOverlap),
		}
	}

	// If new is entirely contained in old then return old[cut], new. old[offset]
	if newStart > oldStart && newEnd < oldEnd {
		// Find the overlap at the start
		startOverlap := newStart - oldStart

		// Find the overlap at the end.
		endOverlap := old.size - (oldEnd - newEnd)

		// slog.Info("case 4", "old", old, "new", new, "startOverlap", startOverlap, "endOverlap", endOverlap)

		return []*regionFragment{
			old.cutAt(startOverlap),
			new,
			old.offsetAt(endOverlap),
		}
	}

	// If old and new overlap on the end only then return [old, new].
	// This also handles the case that new ends at old's end.
	if newStart > oldStart && newEnd >= oldEnd {
		overlap := newStart - oldStart

		// slog.Info("case 5", "old", old, "new", new, "overlap", overlap)

		return []*regionFragment{
			old.cutAt(overlap),
			new,
		}
	}

	slog.Info("unimplemented", "oldStart", oldStart, "oldEnd", oldEnd, "newStart", newStart, "newEnd", newEnd)

	panic("unimplemented")
}

// This is a implementation detail so the struct is private.
type fragmentedRegion struct {
	// This array should always be sorted according to offset.
	fragments []*regionFragment
	totalSize uint64
}

func (f *fragmentedRegion) String() string {
	s := "[ "

	for _, region := range f.fragments {
		s += fmt.Sprintf("%+v %08x-%08x ", region.region, region.offset, region.offset+int64(region.size))
	}

	s += "]"

	return s
}

// ReadAt implements MemoryRegion.
func (f *fragmentedRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(f, off); err != nil {
		return 0, err
	}

	for _, frag := range f.fragments {
		// If the fragment doesn't overlap with the read area then skip it.
		if (frag.offset + int64(frag.size)) <= off {
			continue
		}

		fragOffset := int64(off) - frag.offset

		var childN int

		readFragment := len(p)

		// If the read fragment is bigger than the remaining fragment size then decrease it to that.
		if readFragment > int(frag.size-fragOffset) {
			readFragment = int(frag.size - fragOffset)
		}

		childN, err = frag.region.ReadAt(p[:readFragment], int64(fragOffset))
		n += childN
		if err != nil {
			return
		}

		p = p[childN:]
		off = int64(frag.offset) + int64(frag.size)

		if len(p) == 0 {
			break
		}
	}

	return
}

// Size implements MemoryRegion.
func (f *fragmentedRegion) Size() int64 {
	return int64(f.totalSize)
}

// WriteAt implements MemoryRegion.
func (f *fragmentedRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(f, off); err != nil {
		return 0, err
	}

	for _, frag := range f.fragments {
		// If the fragment doesn't overlap with the read area then skip it.
		if (frag.offset + int64(frag.size)) <= off {
			continue
		}

		fragOffset := int64(off) - frag.offset

		var childN int

		if len(p) > int(frag.size) {
			childN, err = frag.region.WriteAt(p[:frag.size], int64(fragOffset))
		} else {
			childN, err = frag.region.WriteAt(p, int64(fragOffset))
		}

		n += childN
		if err != nil {
			return
		}

		p = p[childN:]
		off = int64(frag.offset) + int64(frag.size)

		if len(p) == 0 {
			break
		}
	}

	return
}

func (f *fragmentedRegion) mapFragment(frag MemoryRegion, off int64) error {
	// Check that the offset is not more than the maximum size.
	if off > int64(f.totalSize) {
		return fmt.Errorf("off > int64(f.totalSize)")
	}

	// create the new fragmentRegion that needs to be inserted.
	newFragment := &regionFragment{
		region: frag,
		offset: off,
		size:   frag.Size(),
	}

	var newFrags []*regionFragment

	for _, frag := range f.fragments {
		// If there's already a fragment then check if we can skip it.
		if len(newFrags) > 0 {
			last := newFrags[len(newFrags)-1]

			if last.end() > frag.start() {
				if frag.end() > last.end() {
					overlap := frag.size - (frag.end() - last.end())

					// slog.Info("case 6", "last", last, "frag", frag, "overlap", overlap, "overlap_part", (frag.end() - last.end()))

					newFrags = append(newFrags, frag.offsetAt(overlap))
				}

				continue
			}
		}

		newFrags = append(newFrags, remapRegion(frag, newFragment)...)
	}

	f.fragments = newFrags

	return nil
}

func regionToString(region MemoryRegion) string {
	switch region := region.(type) {
	case *OffsetRegion:
		regionStr := regionToString(region.base)
		if regionStr != "" {
			return fmt.Sprintf("%s offset=%016X", regionStr, region.offset)
		} else {
			return ""
		}
	case *PaddedRegion:
		regionStr := regionToString(region.Region)
		if regionStr != "" {
			return fmt.Sprintf("%s padded=%016X", regionStr, region.RegionSize)
		} else {
			return ""
		}
	default:
		return fmt.Sprintf("%+v", region)
	}
}

func (f *fragmentedRegion) dumpMap(out io.Writer, off uint64) error {
	for _, frag := range f.fragments {
		str := regionToString(frag.region)

		if str != "" {
			line := fmt.Sprintf("  %016X: %s", off+uint64(frag.offset), regionToString(frag.region))

			if _, err := fmt.Fprintf(out, "%s\n", line); err != nil {
				return err
			}
		}
	}

	return nil
}

func newFragmentRegion(pageSize uint32) *fragmentedRegion {
	return &fragmentedRegion{
		fragments: []*regionFragment{
			{region: make(RawRegion, pageSize), offset: 0, size: int64(pageSize)},
		},
		totalSize: uint64(pageSize),
	}
}

var (
	_ MemoryRegion = &fragmentedRegion{}
)
