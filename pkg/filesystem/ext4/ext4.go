// Based on: https://www.kernel.org/doc/html/latest/filesystems/ext4/index.html

package ext4

import (
	"fmt"
	"io"
	goFs "io/fs"
	"math"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/exp/constraints"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/path"
)

const DEFAULT_MODE = goFs.FileMode(0755)

const INODE_SIZE = 256

type InodeFlags uint32

const (
	InodeFlag_SECRM            InodeFlags = 0b1
	InodeFlag_UNRM             InodeFlags = 0b10
	InodeFlag_COMPR            InodeFlags = 0b100
	InodeFlag_SYNC             InodeFlags = 0b1000
	InodeFlag_IMMUTABLE        InodeFlags = 0b10000
	InodeFlag_APPEND           InodeFlags = 0b100000
	InodeFlag_NODUMP           InodeFlags = 0b1000000
	InodeFlag_NOATIME          InodeFlags = 0b10000000
	InodeFlag_DIRTY            InodeFlags = 0b100000000
	InodeFlag_COMPRBLK         InodeFlags = 0b1000000000
	InodeFlag_NOCOMPR          InodeFlags = 0b10000000000
	InodeFlag_ENCRYPT          InodeFlags = 0b100000000000
	InodeFlag_INDEX            InodeFlags = 0b1000000000000
	InodeFlag_IMAGIC           InodeFlags = 0b10000000000000
	InodeFlag_JOURNAL_DATA     InodeFlags = 0b100000000000000
	InodeFlag_NOTAIL           InodeFlags = 0b1000000000000000
	InodeFlag_DIRSYNC          InodeFlags = 0b10000000000000000
	InodeFlag_TOPDIR           InodeFlags = 0b100000000000000000
	InodeFlag_HUGE_FILE        InodeFlags = 0b1000000000000000000
	InodeFlag_EXTENTS          InodeFlags = 0b10000000000000000000
	InodeFlag_EA_INODE         InodeFlags = 0b1000000000000000000000
	InodeFlag_EOFBLOCKS        InodeFlags = 0b10000000000000000000000
	InodeFlag_SNAPFILE         InodeFlags = 0b1000000000000000000000000
	InodeFlag_SNAPFILE_DELETED InodeFlags = 0b100000000000000000000000000
	InodeFlag_SNAPFILE_SHRUNK  InodeFlags = 0b1000000000000000000000000000
	InodeFlag_INLINE_DATA      InodeFlags = 0b10000000000000000000000000000
	InodeFlag_PROJINHERIT      InodeFlags = 0b100000000000000000000000000000
	InodeFlag_RESERVED         InodeFlags = 0b10000000000000000000000000000000
)

type Feature_compat uint32

const (
	Feature_compat_COMPAT_DIR_PREALLOC   Feature_compat = 0b1
	Feature_compat_COMPAT_IMAGIC_INODES  Feature_compat = 0b10
	Feature_compat_COMPAT_HAS_JOURNAL    Feature_compat = 0b100
	Feature_compat_COMPAT_EXT_ATTR       Feature_compat = 0b1000
	Feature_compat_COMPAT_RESIZE_INODE   Feature_compat = 0b10000
	Feature_compat_COMPAT_DIR_INDEX      Feature_compat = 0b100000
	Feature_compat_COMPAT_LAZY_BG        Feature_compat = 0b1000000
	Feature_compat_COMPAT_EXCLUDE_INODE  Feature_compat = 0b10000000
	Feature_compat_COMPAT_EXCLUDE_BITMAP Feature_compat = 0b100000000
	Feature_compat_COMPAT_SPARSE_SUPER2  Feature_compat = 0b1000000000
)

type Feature_incompat uint32

const (
	Feature_incompat_INCOMPAT_COMPRESSION Feature_incompat = 0x1
	Feature_incompat_INCOMPAT_FILETYPE    Feature_incompat = 0x2
	Feature_incompat_INCOMPAT_RECOVER     Feature_incompat = 0x4
	Feature_incompat_INCOMPAT_JOURNAL_DEV Feature_incompat = 0x8
	Feature_incompat_INCOMPAT_META_BG     Feature_incompat = 0x10
	Feature_incompat_INCOMPAT_EXTENTS     Feature_incompat = 0x40
	Feature_incompat_INCOMPAT_64BIT       Feature_incompat = 0b10000000
	Feature_incompat_INCOMPAT_MMP         Feature_incompat = 0b100000000
	Feature_incompat_INCOMPAT_FLEX_BG     Feature_incompat = 0b1000000000
	Feature_incompat_INCOMPAT_EA_INODE    Feature_incompat = 0b10000000000
	Feature_incompat_INCOMPAT_DIRDATA     Feature_incompat = 0b1000000000000
	Feature_incompat_INCOMPAT_CSUM_SEED   Feature_incompat = 0b10000000000000
	Feature_incompat_INCOMPAT_LARGEDIR    Feature_incompat = 0b100000000000000
	Feature_incompat_INCOMPAT_INLINE_DATA Feature_incompat = 0b1000000000000000
	Feature_incompat_INCOMPAT_ENCRYPT     Feature_incompat = 0b10000000000000000
)

type Feature_ro_compat uint32

const (
	Feature_ro_compat_RO_COMPAT_SPARSE_SUPER  Feature_ro_compat = 0b1
	Feature_ro_compat_RO_COMPAT_LARGE_FILE    Feature_ro_compat = 0b10
	Feature_ro_compat_RO_COMPAT_BTREE_DIR     Feature_ro_compat = 0b100
	Feature_ro_compat_RO_COMPAT_HUGE_FILE     Feature_ro_compat = 0b1000
	Feature_ro_compat_RO_COMPAT_GDT_CSUM      Feature_ro_compat = 0b10000
	Feature_ro_compat_RO_COMPAT_DIR_NLINK     Feature_ro_compat = 0b100000
	Feature_ro_compat_RO_COMPAT_EXTRA_ISIZE   Feature_ro_compat = 0b1000000
	Feature_ro_compat_RO_COMPAT_HAS_SNAPSHOT  Feature_ro_compat = 0b10000000
	Feature_ro_compat_RO_COMPAT_QUOTA         Feature_ro_compat = 0b100000000
	Feature_ro_compat_RO_COMPAT_BIGALLOC      Feature_ro_compat = 0b1000000000
	Feature_ro_compat_RO_COMPAT_METADATA_CSUM Feature_ro_compat = 0b10000000000
	Feature_ro_compat_RO_COMPAT_REPLICA       Feature_ro_compat = 0b100000000000
	Feature_ro_compat_RO_COMPAT_READONLY      Feature_ro_compat = 0b1000000000000
	Feature_ro_compat_RO_COMPAT_PROJECT       Feature_ro_compat = 0b10000000000000
)

func resolveRelative(wd string, target string) (string, error) {
	if strings.HasPrefix(target, ".") {
		return "", fmt.Errorf("resolveRelative(%+v, %+v) not implemented", wd, target)
	} else if strings.HasPrefix(target, "/") {
		return target, nil
	} else {
		return path.Unix.Join(path.Unix.Dir(wd), target), nil
	}
}

type Directory interface {
	String() string
	AddEntry(child *InodeWrapper, name string) error
	GetChild(name string) (*InodeWrapper, error)
}

type DirectoryEntry struct {
	*vm.PaddedRegion

	offset int64
	ent    *DirEntry2
	name   string
	target *InodeWrapper
}

func (ent DirectoryEntry) String() string {
	return fmt.Sprintf("\"% 30s\":0x%X", ent.name, ent.offset)
}

func (ent DirectoryEntry) recordLength() uint16 {
	recLen := uint16(ent.ent.Size()) + uint16(len(ent.name))

	recLen = roundUpDiv(recLen, 4) * 4

	return recLen
}

func (ent *DirectoryEntry) setRecordLength(recLen uint16) {
	ent.ent.SetRecLen(recLen)

	ent.PaddedRegion = vm.NewPaddedRegion(vm.NewRegionArray[vm.MemoryRegion](
		ent.ent,
		vm.RawRegion(ent.name),
	), int64(recLen))
}

func newDirectoryEntry(target *InodeWrapper, childNode uint32, typ uint8, name string, recLen uint16) *DirectoryEntry {
	ent := &DirectoryEntry{
		target: target,
		ent:    &DirEntry2{},
		name:   name,
	}

	ent.ent.SetRecLen(recLen)
	ent.ent.SetInode(childNode)
	ent.ent.SetFileType(typ)
	ent.ent.SetNameLen(uint8(len(ent.name)))

	ent.PaddedRegion = vm.NewPaddedRegion(vm.NewRegionArray[vm.MemoryRegion](
		ent.ent,
		vm.RawRegion(name),
	), int64(recLen))

	return ent
}

type LinearDirectoryBlock struct {
	ents         *vm.RegionArray[*DirectoryEntry]
	paddedRegion *vm.PaddedRegion
}

type LinearDirectory struct {
	fs         *Ext4Filesystem
	extentTree ExtentTree
	inode      *InodeWrapper
	ents       map[string]*DirectoryEntry
	blocks     []*LinearDirectoryBlock
}

// GetChild implements Directory.
func (d *LinearDirectory) GetChild(name string) (*InodeWrapper, error) {
	ent, ok := d.ents[name]
	if !ok {
		return nil, os.ErrNotExist
	}

	return ent.target, nil
}

// AddEntry implements Directory.
func (d *LinearDirectory) AddEntry(child *InodeWrapper, name string) error {
	if _, exists := d.ents[name]; exists {
		return filesystem.ErrExist{Name: name}
	}

	blockSize := child.fs.sb.blockSize()

	// For "." and "..", we need special handling
	var typ uint8
	if name == "." || name == ".." {
		// Both "." and ".." always point to directories
		typ = 0x2 // Directory type
	} else {
		// For regular entries, determine type from the child's mode
		childMode := child.Mode()

		typ = 0x1 // Default to regular file

		if childMode&goFs.ModeSymlink != 0 {
			typ = 0x7 // Symbolic link
		} else if childMode.IsDir() {
			typ = 0x2 // Directory
		} else if childMode&goFs.ModeCharDevice != 0 {
			typ = 0x3 // Character device
		} else if childMode&goFs.ModeDevice != 0 {
			typ = 0x4 // Block device
		} else if childMode&goFs.ModeNamedPipe != 0 {
			typ = 0x5 // FIFO
		} else if childMode&goFs.ModeSocket != 0 {
			typ = 0x6 // Socket
		}
		// Regular file remains as typ = 0x1
	}

	block := d.blocks[len(d.blocks)-1]

	var ent *DirectoryEntry

	if block.ents.Len() == 0 {
		ent = newDirectoryEntry(child, uint32(child.num), uint8(typ), name, uint16(blockSize))
		block.ents.Append(ent)
	} else {
		lastEnt := block.ents.Get(block.ents.Len() - 1)
		currentLen := lastEnt.ent.RecLen()

		requiredLen := roundUpDiv(8+len(name), 4) * 4

		lastRecLen := lastEnt.recordLength()

		// Make sure the directory entry has enough room to add the new entry.
		if int(currentLen-lastRecLen) < requiredLen {
			// Otherwise increase the size.
			if err := d.increaseSize(); err != nil {
				return fmt.Errorf("failed to increase size: %v", err)
			}

			// Call the method again.
			return d.AddEntry(child, name)
		}

		ent = newDirectoryEntry(child, uint32(child.num), uint8(typ), name, currentLen-lastRecLen)

		block.ents.Append(ent)

		lastEnt.setRecordLength(lastRecLen)
	}

	d.ents[name] = ent

	return nil
}

func (d LinearDirectory) String() string {
	ret := "Directory ["

	for _, block := range d.blocks {
		ret += "\n   Block ["
		block.ents.Range(func(i int, ent *DirectoryEntry) bool {
			ret += "\n    " + ent.String() + ","
			return true
		})
		ret += "\n   ]"
	}

	return ret + "\n  ]"
}

func (d *LinearDirectory) increaseSize() error {
	// Get the extents.
	extents, err := d.extentTree.Extents()
	if err != nil {
		return err
	}

	// Split the extents into a list of blocks.
	extents = splitExtentIntoBlocks(extents)

	if len(extents) == len(d.blocks) {
		// Allocate more blocks.
		// The block allocation goes 1,4,16,64 blocks.
		if err = d.extentTree.AllocateBlocks(int64(len(d.blocks) * 4)); err != nil {
			log.Default().Warn("failed to allocate blocks", "blocks", int64(len(d.blocks)*2))
			return err
		}

		// Get the new list of extents.
		extents, err = d.extentTree.Extents()
		if err != nil {
			return err
		}

		// For each new node mark it with a empty directory entry filling up the entire node.
		lastExtent := extents[len(extents)-1]

		lastExtentBlocks := splitExtentIntoBlocks([]Extent{lastExtent})

		for i, block := range lastExtentBlocks {
			if i == 0 {
				continue // skip the first one as a optimization.
			}

			ent := &DirEntry2{}

			ent.SetRecLen(uint16(d.fs.sb.blockSize()))

			if err := d.fs.mapRawExtent(
				ent,
				&block,
			); err != nil {
				return err
			}
		}

		// Split the extents into a list of blocks.
		extents = splitExtentIntoBlocks(extents)
	}

	// Get the next extent for the directory block.
	nextExtent := extents[len(d.blocks)]

	// Add the block to the directory.
	if err := d.addBlock(nextExtent); err != nil {
		return err
	}

	// Update the size in the node.
	size := uint64(len(extents)) * d.fs.sb.blockSize()
	d.inode.node.SetSizeLo(uint32(size & 0xFFFFFFFF))
	d.inode.node.SetSizeHigh(uint32(size >> 32))
	blocks := (uint64(len(extents)) * d.fs.sb.blockSize()) / 512
	d.inode.node.SetBlocksLo(uint32(blocks & 0xFFFFFFFF))
	d.inode.node.SetBlocksHigh(uint16(blocks >> 32))

	return nil
}

func (dir *LinearDirectory) addBlock(extent Extent) error {
	block := &LinearDirectoryBlock{ents: vm.NewRegionArray[*DirectoryEntry]()}

	// Create a padded region to store the directory data.
	block.paddedRegion = vm.NewPaddedRegion(
		block.ents,
		int64(extent.Length)*int64(dir.fs.sb.blockSize()),
	)

	// Map the directory to the virtual filesystem.
	if err := dir.fs.mapRawExtent(
		block.paddedRegion,
		&extent,
	); err != nil {
		return err
	}

	dir.blocks = append(dir.blocks, block)

	return nil
}

func newLinearDirectory(fs *Ext4Filesystem, inode *InodeWrapper, tree ExtentTree) (*LinearDirectory, error) {
	dir := &LinearDirectory{
		fs:         fs,
		extentTree: tree,
		inode:      inode,
		blocks:     []*LinearDirectoryBlock{},
		ents:       make(map[string]*DirectoryEntry),
	}

	// Get the extent list.
	extents, err := tree.Extents()
	if err != nil {
		return nil, err
	}

	// Assume that there is only 1 extent in the list.
	if len(extents) != 1 {
		return nil, fmt.Errorf("newLinearDirectory does not support multi-extent inodes")
	}

	extent := extents[0]

	if err := dir.addBlock(extent); err != nil {
		return nil, err
	}

	return dir, nil
}

type HashedDirectory struct {
}

// GetChild implements Directory.
func (*HashedDirectory) GetChild(name string) (*InodeWrapper, error) {
	return nil, fmt.Errorf("unimplemented")
}

// AddEntry implements Directory.
func (*HashedDirectory) AddEntry(child *InodeWrapper, name string) error {
	return fmt.Errorf("not implemented")
}

func (d HashedDirectory) String() string {
	ret := "Directory["

	ret += "\n  HASHED"

	return ret + "\n  ]"
}

var (
	_ Directory = &LinearDirectory{}
	_ Directory = &HashedDirectory{}
)

type Extent struct {
	FirstFileBlock uint32
	StartBlock     uint64
	Length         uint16
}

func (e *Extent) String() string {
	return fmt.Sprintf("Extent{%d %d-%d}", e.FirstFileBlock, e.StartBlock, e.Length)
}

func NewExtent(firstFileBlock uint32, startBlock uint64, length uint16) (Extent, error) {
	ext := Extent{
		FirstFileBlock: firstFileBlock,
		StartBlock:     startBlock,
		Length:         length,
	}

	if startBlock == 0 {
		return ext, fmt.Errorf("NewExtent startBlock == 0 (null pointer)")
	}

	return ext, nil
}

// Split a list of extents into a series of virtual extents with 1 extent per block.
func splitExtentIntoBlocks(ext []Extent) []Extent {
	var ret []Extent

	for _, extent := range ext {
		for i := 0; i < int(extent.Length); i++ {
			ret = append(ret, Extent{
				FirstFileBlock: extent.FirstFileBlock + uint32(i),
				StartBlock:     extent.StartBlock + uint64(i),
				Length:         1,
			})
		}
	}

	return ret
}

type ExtentTree interface {
	Extents() ([]Extent, error)
	AllocateBlocks(blocks int64) error
}

type ExtentTree2 struct {
	i     *InodeWrapper
	count int
}

// AllocateBlocks implements ExtentTree.
func (t *ExtentTree2) AllocateBlocks(blocks int64) error {
	// Check that we have enough space in the extentTree to allocate blocks.
	if t.count > 3 {
		return fmt.Errorf("no remaining space in extent tree to allocate blocks")
	}

	// Allocate more extents.
	extents, err := t.i.fs.allocateMultiExtentBlocks(blocks)
	if err != nil {
		return err
	}

	if len(extents)+t.count > 4 {
		return fmt.Errorf("allocation would exceed the size constraint of a ExtentTree2")
	}

	i := 0

	if t.count == 1 && i < len(extents) {
		extent := extents[i]

		// Set the fields on the leaf.
		t.i.node.SetBlock1Block(t.i.node.Block0Block() + uint32(t.i.node.Block0Len()) + extent.FirstFileBlock)
		t.i.node.SetBlock1Len(extent.Length)
		t.i.node.SetBlock1StartLo(uint32(extent.StartBlock))
		t.i.node.SetBlock1StartHi(uint16(extent.StartBlock >> 32))

		t.i.node.SetBlockEntries(2)

		i += 1
		t.count += 1
	}

	if t.count == 2 && i < len(extents) {
		extent := extents[i]

		// Set the fields on the leaf.
		t.i.node.SetBlock2Block(t.i.node.Block1Block() + uint32(t.i.node.Block1Len()) + extent.FirstFileBlock)
		t.i.node.SetBlock2Len(extent.Length)
		t.i.node.SetBlock2StartLo(uint32(extent.StartBlock))
		t.i.node.SetBlock2StartHi(uint16(extent.StartBlock >> 32))

		t.i.node.SetBlockEntries(3)

		i += 1
		t.count += 1
	}

	if t.count == 3 && i < len(extents) {
		extent := extents[i]

		// Set the fields on the leaf.
		t.i.node.SetBlock3Block(t.i.node.Block2Block() + uint32(t.i.node.Block2Len()) + extent.FirstFileBlock)
		t.i.node.SetBlock3Len(extent.Length)
		t.i.node.SetBlock3StartLo(uint32(extent.StartBlock))
		t.i.node.SetBlock3StartHi(uint16(extent.StartBlock >> 32))

		t.i.node.SetBlockEntries(4)

		t.count += 1
	}

	return nil
}

// Extents implements ExtentTree.
func (t *ExtentTree2) Extents() ([]Extent, error) {
	var ret []Extent

	if t.count > 0 {
		ext, err := NewExtent(
			t.i.node.Block0Block(),
			uint64(t.i.node.Block0Start()),
			t.i.node.Block0Len(),
		)
		if err != nil {
			return nil, err
		}
		ret = append(ret, ext)
	}

	if t.count > 1 {
		ext, err := NewExtent(
			t.i.node.Block1Block(),
			uint64(t.i.node.Block1Start()),
			t.i.node.Block1Len(),
		)
		if err != nil {
			return nil, err
		}
		ret = append(ret, ext)
	}

	if t.count > 2 {
		ext, err := NewExtent(
			t.i.node.Block2Block(),
			uint64(t.i.node.Block2Start()),
			t.i.node.Block2Len(),
		)
		if err != nil {
			return nil, err
		}
		ret = append(ret, ext)
	}

	if t.count > 3 {
		ext, err := NewExtent(
			t.i.node.Block3Block(),
			uint64(t.i.node.Block3Start()),
			t.i.node.Block3Len(),
		)
		if err != nil {
			return nil, err
		}
		ret = append(ret, ext)
	}

	return ret, nil
}

func newExtentTree2(fs *Ext4Filesystem, i *InodeWrapper, blocks int64) (*ExtentTree2, error) {
	tree := &ExtentTree2{i: i, count: 1}

	// Allocate blocks.
	extents, err := fs.allocateMultiExtentBlocks(blocks)
	if err != nil {
		return nil, err
	}

	if len(extents) > 4 {
		panic("newExtentTree2 called with more than 4 extents.")
	}

	// Set the fields on the header.
	i.node.SetBlockMagic(0xF30A)
	i.node.SetBlockEntries(uint16(len(extents)))
	i.node.SetBlockMax(4)
	i.node.SetBlockDepth(0)

	if len(extents) > 0 {
		extent := extents[0]

		// Set the fields on the leaf.
		i.node.SetBlock0Block(extent.FirstFileBlock)
		i.node.SetBlock0Len(extent.Length)
		i.node.SetBlock0StartLo(uint32(extent.StartBlock))
		i.node.SetBlock0StartHi(uint16(extent.StartBlock >> 32))
	}

	if len(extents) > 1 {
		extent := extents[1]

		// Set the fields on the leaf.
		i.node.SetBlock1Block(extent.FirstFileBlock)
		i.node.SetBlock1Len(extent.Length)
		i.node.SetBlock1StartLo(uint32(extent.StartBlock))
		i.node.SetBlock1StartHi(uint16(extent.StartBlock >> 32))
	}

	if len(extents) > 2 {
		extent := extents[2]

		// Set the fields on the leaf.
		i.node.SetBlock2Block(extent.FirstFileBlock)
		i.node.SetBlock2Len(extent.Length)
		i.node.SetBlock2StartLo(uint32(extent.StartBlock))
		i.node.SetBlock2StartHi(uint16(extent.StartBlock >> 32))
	}

	if len(extents) > 3 {
		extent := extents[3]

		// Set the fields on the leaf.
		i.node.SetBlock3Block(extent.FirstFileBlock)
		i.node.SetBlock3Len(extent.Length)
		i.node.SetBlock3StartLo(uint32(extent.StartBlock))
		i.node.SetBlock3StartHi(uint16(extent.StartBlock >> 32))
	}

	tree.count = len(extents)

	return tree, nil
}

var (
	_ ExtentTree = &ExtentTree2{}
)

// ExtentTreeIdxDepth1 implements a single-level indexed extent tree (depth=1).
// The inode stores an index entry pointing to a leaf block that contains all
// the extents for the file.
type ExtentTreeIdxDepth1 struct {
	i       *InodeWrapper
	leafBlk uint64
	ext     []Extent
}

func (t *ExtentTreeIdxDepth1) Extents() ([]Extent, error) {
	return append([]Extent(nil), t.ext...), nil
}

func (t *ExtentTreeIdxDepth1) AllocateBlocks(blocks int64) error {
	// Allocate additional data extents and rebuild the leaf block mapping.
	exts, err := t.i.fs.allocateMultiExtentBlocks(blocks)
	if err != nil {
		return err
	}
	t.ext = append(t.ext, func(in []*Extent) []Extent {
		out := make([]Extent, 0, len(in))
		for _, e := range in {
			out = append(out, *e)
		}
		return out
	}(exts)...)

	return mapExtentLeafBlock(t.i.fs, t.leafBlk, t.ext)
}

// mapExtentLeafBlock creates or updates a leaf extent block on disk at the
// given block number with the provided extents.
func mapExtentLeafBlock(fs *Ext4Filesystem, leafBlk uint64, exts []Extent) error {
	// Build header for a leaf node.
	hdr := &ExtentTreeHeader{}
	hdr.SetMagic(0xF30A)
	hdr.SetDepth(0)
	hdr.SetEntries(uint16(len(exts)))
	// Calculate capacity for a full block of entries.
	// Each entry is 12 bytes; header is 12 bytes.
	max := (fs.sb.blockSize() - uint64(hdr.Size())) / uint64(NewExtentTreeNode().Size())
	if max > 0xFFFF {
		max = 0xFFFF
	}
	hdr.SetMax(uint16(max))

	// Build entries
	var nodes vm.RegionArray[*ExtentTreeNode]
	for _, e := range exts {
		n := &ExtentTreeNode{}
		n.SetBlock(e.FirstFileBlock)
		n.SetLen(e.Length)
		n.SetStartLo(uint32(e.StartBlock))
		n.SetStartHi(uint16(e.StartBlock >> 32))
		nodes.Append(n)
	}

	arr := vm.NewRegionArray[vm.MemoryRegion](hdr, &nodes)
	padded := vm.NewPaddedRegion(arr, int64(fs.sb.blockSize()))

	// Map to disk at leafBlk
	return fs.mapRegion(padded, int64(leafBlk*fs.sb.blockSize()))
}

func newExtentTreeIdxDepth1(fs *Ext4Filesystem, i *InodeWrapper, blocks int64) (*ExtentTreeIdxDepth1, error) {
	// Allocate file data extents across block groups.
	exts, err := fs.allocateMultiExtentBlocks(blocks)
	if err != nil {
		return nil, err
	}

	// Convert to value slice for storage.
	vals := make([]Extent, 0, len(exts))
	for _, e := range exts {
		vals = append(vals, *e)
	}

	// Allocate one block for the leaf node (more than enough for our extents).
	leaf, err := fs.allocateBlocks(1)
	if err != nil {
		return nil, err
	}

	// Map the leaf block contents with all extents.
	if err := mapExtentLeafBlock(fs, leaf.StartBlock, vals); err != nil {
		return nil, err
	}

	// Point inode header at the leaf via an index entry.
	i.node.SetBlockMagic(0xF30A)
	i.node.SetBlockDepth(1)
	i.node.SetBlockMax(4) // inode can store up to 4 index entries
	i.node.SetBlockEntries(1)

	// index entry 0 encoding in inode (12 bytes):
	// [0..4)  ei_block (first logical file block covered)
	// [4..8)  ei_leaf_lo (low 32 bits of leaf physical block)
	// [8..10) ei_leaf_hi (high 16 bits)
	// [10..12) ei_unused
	first := uint32(0)
	if len(vals) > 0 {
		first = vals[0].FirstFileBlock
	}
	leafLo := uint32(leaf.StartBlock)
	leafHi := uint16(leaf.StartBlock >> 32)

	i.node.SetBlock0Block(first)
	// Write ei_leaf_lo across the 4 bytes starting at offset 56.
	i.node.SetBlock0Len(uint16(leafLo & 0xFFFF))
	i.node.SetBlock0StartHi(uint16((leafLo >> 16) & 0xFFFF))
	// Write ei_leaf_hi in low 16 bits at offset 60; high 16 bits (unused) = 0.
	i.node.SetBlock0StartLo(uint32(leafHi))

	return &ExtentTreeIdxDepth1{i: i, leafBlk: leaf.StartBlock, ext: vals}, nil
}

func newExtentTree(fs *Ext4Filesystem, i *InodeWrapper, blocks int64) (ExtentTree, error) {
	blockGroupSize := int64(fs.sb.BlocksPerGroup())

	requiredBlockGroups := roundUpDiv(blocks, blockGroupSize)

	// log.Default().Info("", "requiredBlockGroups", requiredBlockGroups)

	if requiredBlockGroups <= 4 {
		return newExtentTree2(fs, i, blocks)
	} else {
		// Use a depth-1 indexed extent tree for larger files.
		return newExtentTreeIdxDepth1(fs, i, blocks)
	}
}

type InodeWrapper struct {
	fs         *Ext4Filesystem
	bg         *BlockGroup
	offset     uint64
	num        int
	node       *Inode
	extentTree ExtentTree
	dir        Directory
	linkTarget string
}

const (
	S_IFBLK  = 0x6000
	S_IFCHR  = 0x2000
	S_IFDIR  = 0x4000
	S_IFIFO  = 0x1000
	S_IFLNK  = 0xa000
	S_IFMT   = 0xf000
	S_IFREG  = 0x8000
	S_IFSOCK = 0xc000
	S_ISGID  = 0x400
	S_ISUID  = 0x800
	S_ISVTX  = 0x200
)

func (i InodeWrapper) getModeType() goFs.FileMode {
	sysMode := i.node.Mode()

	mode := goFs.FileMode(0)

	switch sysMode & S_IFMT {
	case S_IFBLK:
		mode |= goFs.ModeDevice
	case S_IFCHR:
		mode |= goFs.ModeDevice | goFs.ModeCharDevice
	case S_IFDIR:
		mode |= goFs.ModeDir
	case S_IFIFO:
		mode |= goFs.ModeNamedPipe
	case S_IFLNK:
		mode |= goFs.ModeSymlink
	case S_IFREG:
		// nothing to do
	case S_IFSOCK:
		mode |= goFs.ModeSocket
	}
	if sysMode&S_ISGID != 0 {
		mode |= goFs.ModeSetgid
	}
	if sysMode&S_ISUID != 0 {
		mode |= goFs.ModeSetuid
	}
	if sysMode&S_ISVTX != 0 {
		mode |= goFs.ModeSticky
	}

	return mode
}

func (i InodeWrapper) Mode() goFs.FileMode {
	sysMode := i.node.Mode()

	mode := goFs.FileMode(sysMode&0777) | i.getModeType()

	return mode
}

func (i InodeWrapper) Flags() InodeFlags {
	return InodeFlags(i.node.Flags())
}

func (i InodeWrapper) String() string {
	mode := i.Mode()
	return fmt.Sprintf(
		"Inode {\n  inode = %d, offset = %d, size = %d, mode = %s, isDir = %+v, blocks = %d, flags = %032b\n  tree = %+v,\n  dir = %s\n}",
		i.num, i.offset, i.node.NSize(), mode, mode.IsDir(), i.node.Blocks(), i.Flags(), i.extentTree, i.dir,
	)
}

func (i *InodeWrapper) allocateExtent(blocks int64) error {
	extentTree, err := newExtentTree(i.fs, i, blocks)
	if err != nil {
		return err
	}

	i.extentTree = extentTree

	// Set the flag to identify that this node uses extents.
	i.node.SetFlags(0x80000)

	return nil
}

// In allocateDirectory method, change the initial link count:
func (i *InodeWrapper) allocateDirectory(parent *InodeWrapper) error {
	// set the directory flag.
	i.node.SetMode(i.node.Mode() | S_IFDIR)

	if !i.fs.deterministicTime.IsZero() {
		i.node.SetCtime(uint32(i.fs.deterministicTime.Unix()))
		i.node.SetMtime(uint32(i.fs.deterministicTime.Unix()))
		i.node.SetAtime(uint32(i.fs.deterministicTime.Unix()))
	} else {
		i.node.SetCtime(uint32(time.Now().Unix()))
		i.node.SetMtime(uint32(time.Now().Unix()))
		i.node.SetAtime(uint32(time.Now().Unix()))
	}

	// allocate a block to store the directory listing.
	if err := i.allocateExtent(1); err != nil {
		return err
	}

	i.node.SetSizeLo(4096)
	i.node.SetSizeHigh(0)
	i.node.SetBlocksLo(8)
	i.node.SetBlocksHigh(0)

	// Map the extent as a directory.
	dir, err := newLinearDirectory(i.fs, i, i.extentTree)
	if err != nil {
		return err
	}
	i.dir = dir

	// Directories start with link count 2 (for "." and the parent's entry)
	if i.num == 2 {
		i.node.SetLinksCount(2)
	} else {
		i.node.SetLinksCount(1)
	}

	// Add the `.` and `..` directories
	if err := i.dir.AddEntry(i, "."); err != nil {
		return err
	}
	if err := i.dir.AddEntry(parent, ".."); err != nil {
		return err
	}

	// Update the directory count in the bgd.
	current := i.bg.desc.BgUsedDirsCount()
	i.bg.desc.SetUsedDirsCountLo(uint16(current + 1))
	i.bg.desc.SetUsedDirsCountHi(uint16((current + 1) >> 16))

	return nil
}

func (i *InodeWrapper) getChild(name string) (*InodeWrapper, error) {
	if !i.Mode().IsDir() {
		return nil, goFs.ErrInvalid
	}

	child, err := i.dir.GetChild(name)
	if err != nil {
		return nil, err
	}

	return child, nil
}

var (
	totalAddContents int64 = 0
)

func (i *InodeWrapper) addContents(contents vm.MemoryRegion, symlink bool) error {
	start := time.Now()

	// set the file flag.
	if symlink {
		i.node.SetMode(i.node.Mode() | S_IFLNK)
	} else {
		i.node.SetMode(i.node.Mode() | S_IFREG)
	}

	// Update the times.
	if !i.fs.deterministicTime.IsZero() {
		i.node.SetCtime(uint32(i.fs.deterministicTime.Unix()))
		i.node.SetMtime(uint32(i.fs.deterministicTime.Unix()))
		i.node.SetAtime(uint32(i.fs.deterministicTime.Unix()))
	} else {
		i.node.SetCtime(uint32(time.Now().Unix()))
		i.node.SetMtime(uint32(time.Now().Unix()))
		i.node.SetAtime(uint32(time.Now().Unix()))
	}

	if symlink && contents.Size() < 60 {
		if err := i.fs.mapRegion(vm.NewPaddedRegion(contents, 60), int64(i.offset)+40); err != nil {
			return fmt.Errorf("failed to mapRegion: %+v", err)
		}

		size := uint64(contents.Size())
		i.node.SetSizeLo(uint32(size & 0xFFFFFFFF))
		i.node.SetSizeHigh(uint32(size >> 32))

		i.linkTarget = string(contents.(vm.RawRegion))
	} else {
		blocks := roundUpDiv(int(contents.Size()), int(i.fs.sb.blockSize()))

		if err := i.allocateExtent(int64(blocks)); err != nil {
			return fmt.Errorf("failed to allocate extent: %+v", err)
		}

		ext, err := i.extentTree.Extents()
		if err != nil {
			return fmt.Errorf("failed to get extents: %+v", err)
		}

		size := uint64(contents.Size())
		i.node.SetSizeLo(uint32(size & 0xFFFFFFFF))
		i.node.SetSizeHigh(uint32(size >> 32))
		blockCount := (uint64(blocks) * i.fs.sb.blockSize()) / 512
		// Include extent metadata blocks (e.g., one leaf block for depth=1 trees).
		if _, ok := i.extentTree.(*ExtentTreeIdxDepth1); ok {
			blockCount += i.fs.sb.blockSize() / 512
		}
		i.node.SetBlocksLo(uint32(blockCount & 0xFFFFFFFF))
		i.node.SetBlocksHigh(uint16(blockCount >> 32))

		for _, extent := range ext {
			if err := i.fs.mapExtent(contents, &extent); err != nil {
				return fmt.Errorf("failed to addContents: %+v", err)
			}
		}
	}

	totalAddContents += int64(time.Since(start).Nanoseconds())

	return nil
}

func (i *InodeWrapper) chmod(mode goFs.FileMode) error {
	oldMode := i.node.Mode()

	sysPart := oldMode & ^uint16(0777)

	newMode := sysPart | uint16(mode&goFs.ModePerm)

	if mode&goFs.ModeSetuid != 0 {
		newMode |= S_ISUID
	}
	if mode&goFs.ModeSetgid != 0 {
		newMode |= S_ISGID
	}

	i.node.SetMode(newMode)

	// log.Default().Info("", "mode", fmt.Sprintf("%X", i.node.Mode()))

	return nil
}

func (i *InodeWrapper) chown(uid uint16, gid uint16) error {
	i.node.SetUid(uid)
	i.node.SetGid(gid)

	return nil
}

func (i *InodeWrapper) chtime(mod time.Time) error {
	i.node.SetMtime(uint32(mod.Unix()))

	return nil
}

func (i *InodeWrapper) addDirectoryEntry(child *InodeWrapper, name string) error {
	if i.dir == nil {
		return fmt.Errorf("not a directory")
	}

	if err := i.dir.AddEntry(child, name); err != nil {
		return err
	}

	// Only increment link count for regular entries (not "." or "..")
	if name != "." && name != ".." {
		child.node.SetLinksCount(child.node.LinksCount() + 1)

		// If we're adding a directory as a child, increment our link count
		// (for the ".." entry in the child directory)
		if child.Mode().IsDir() {
			i.node.SetLinksCount(i.node.LinksCount() + 1)
		}
	}

	return nil
}

type BlockGroup struct {
	fs *Ext4Filesystem

	num         int
	offset      int64
	desc        *BlockGroupDescriptor
	inodeBitmap *vm.BitmapRegion
	blockBitmap *vm.BitmapRegion
	firstBlock  int64

	inodeCount uint32
	blockCount uint32

	firstFreeInode uint32
	firstFreeBlock uint32
}

func (bg *BlockGroup) allocateBlocks(blocks uint32) (*Extent, error) {
	// Check if this block group even has a chance of fitting all the blocks
	if bg.desc.BgFreeBlocksCount() < blocks {
		return nil, nil
	}

	// Make sure we don't try to allocate beyond the actual block count
	if bg.firstFreeBlock+blocks > bg.blockCount {
		return nil, nil
	}

	var start uint32 = 0
	var noFreeBlocks = true

	// Check if this is a full block group allocation.
	if blocks == bg.blockCount && bg.desc.BgFreeBlocksCount() == bg.blockCount {
		if bg.firstFreeBlock != 0 {
			return nil, nil
		}

		// Set the first free block to the number of blocks.
		bg.firstFreeBlock = bg.blockCount

		// Set the free block count to 0.
		bg.desc.SetFreeBlocksCountLo(0)
		bg.desc.SetFreeBlocksCountHi(0)

		// Fill the entire block bitmap.
		if err := bg.blockBitmap.SetAll(true); err != nil {
			return nil, err
		}

		// Return a new extent.
		ext, err := NewExtent(0, uint64(bg.firstBlock), uint16(blocks))
		if err != nil {
			return nil, fmt.Errorf("failed to allocate full block group: %v", err)
		}

		return &ext, nil
	}

	for i := bg.firstFreeBlock; i < bg.blockCount; i++ {
		used, err := bg.blockBitmap.Get(uint64(i))
		if err != nil {
			return nil, err
		}

		if used {
			if noFreeBlocks {
				bg.firstFreeBlock = i + 1
			}

			start = i + 1

			continue
		} else {
			noFreeBlocks = false
		}

		if i-start != uint32(blocks) {
			continue
		}

		// We found the blocks we need.

		// Reserve them in the bitmap.
		for x := start; x < i; x++ {
			if err := bg.blockBitmap.Set(uint64(x), true); err != nil {
				return nil, err
			}
		}

		// Update the block count.
		current := bg.desc.BgFreeBlocksCount() - uint32(blocks)
		bg.desc.SetFreeBlocksCountLo(uint16(current))
		bg.desc.SetFreeBlocksCountHi(uint16(current >> 16))

		// log.Default().Info("allocated", "start", start, "blocks", blocks)

		// Return the extent.
		ext, err := NewExtent(0, uint64(bg.firstBlock)+uint64(start), uint16(blocks))
		if err != nil {
			// log.Default().Info("",
			// 	"firstBlock", bg.firstBlock,
			// 	"start", start,
			// 	"blocks", blocks,
			// 	"freeBlocks", bg.desc.FreeBlocksCount(),
			// )
			return nil, fmt.Errorf("failed to allocate regular blocks: %v", err)
		}

		return &ext, nil
	}

	return nil, nil
}

func (bg *BlockGroup) allocateInode() (*InodeWrapper, error) {
	if bg.desc.BgFreeInodesCount() == 0 {
		return nil, nil
	}

	for i := bg.firstFreeInode; i < bg.inodeCount; i++ {
		used, err := bg.inodeBitmap.Get(uint64(i))
		if err != nil {
			return nil, err
		}

		if used {
			bg.firstFreeInode = i + 1

			continue
		}

		// Found the inode. Set it in the bitmap.
		if err := bg.inodeBitmap.Set(uint64(i), true); err != nil {
			return nil, err
		}

		// Calculate the inode number and the offset into the inode table.
		inodeNumber := (bg.inodeCount * uint32(bg.num)) + i + 1
		inodeTableStart := uint64(bg.desc.InodeTable()) * bg.fs.sb.blockSize()
		inodeOffset := inodeTableStart + uint64(i*INODE_SIZE)

		// Make the wrapper.
		inode := &InodeWrapper{
			fs:     bg.fs,
			bg:     bg,
			offset: uint64(inodeOffset),
			num:    int(inodeNumber),
			node:   &Inode{},
		}

		if err := inode.chmod(DEFAULT_MODE); err != nil {
			return nil, err
		}

		// log.Default().Info("map inode", "off", inodeOffset, "num", inodeNumber)
		// Map the inode data.
		if err := bg.fs.mapRegion(inode.node, int64(inodeOffset)); err != nil {
			return nil, err
		}

		// Update the inode count.
		current := bg.desc.BgFreeInodesCount() - 1
		bg.desc.SetFreeInodesCountLo(uint16(current))
		bg.desc.SetFreeInodesCountHi(uint16(current >> 16))

		// Return the wrapper.
		return inode, nil
	}

	return nil, nil
}

type Ext4Filesystem struct {
	vm *vm.VirtualMemory

	sb *Superblock

	bgs        []*BlockGroup
	inodes     map[int]*InodeWrapper
	inodeCache map[string]*InodeWrapper

	deterministicTime time.Time
}

func (fs *Ext4Filesystem) allocateMultiExtentBlocks(blocks int64) ([]*Extent, error) {
	blockGroupSize := int64(fs.sb.BlocksPerGroup())
	totalBlockGroups := blocks / blockGroupSize
	remainingBlocks := blocks % blockGroupSize

	var currentBlock uint32 = 0

	var ret []*Extent
	for i := 0; i < int(totalBlockGroups); i++ {
		// TODO(joshua): Add optimized full block group allocator.
		ext, err := fs.allocateBlocks(blockGroupSize)
		if err != nil {
			return nil, err
		}

		ext.FirstFileBlock = currentBlock

		currentBlock += uint32(ext.Length)

		ret = append(ret, ext)
	}

	if remainingBlocks > 0 {
		ext, err := fs.allocateBlocks(remainingBlocks)
		if err != nil {
			return nil, err
		}

		ext.FirstFileBlock = currentBlock

		ret = append(ret, ext)
	}

	// if len(ret) > 1 {
	// 	log.Default().Info("multi extent",
	// 		"totalBlockGroups", totalBlockGroups,
	// 		"remainingBlocks", remainingBlocks,
	// 		"ret", ret,
	// 	)
	// }

	return ret, nil
}

var (
	totalAllocateBlocks int64 = 0
)

func (fs *Ext4Filesystem) allocateBlocks(blocks int64) (*Extent, error) {
	start := time.Now()

	if blocks > int64(fs.sb.BlocksPerGroup()) {
		return nil, fmt.Errorf("requested blocks is larger than can fit in a single group. Use allocateMultiExtentBlocks")
	}

	// Always tries to map contiguous blocks.
	for _, bg := range fs.bgs {
		ext, err := bg.allocateBlocks(uint32(blocks))
		if err != nil {
			return nil, err
		}

		if ext != nil {
			// Update the free block count.
			current := fs.sb.FreeBlocksCount() - uint32(blocks)
			fs.sb.SetFreeBlocksCountLo(current)
			fs.sb.SetFreeBlocksCountHi(0) // Assuming we're not using high part for now

			totalAllocateBlocks += int64(time.Since(start).Nanoseconds())

			return ext, nil
		}
	}

	// TODO(joshua): Add a fallback for mapping from fragments.

	return nil, fmt.Errorf("filesystem is full or fragmented")
}

func (fs *Ext4Filesystem) allocateBlocksForBytes(size int64) (*Extent, error) {
	blocks := roundUpDiv(size, int64(fs.sb.blockSize()))

	return fs.allocateBlocks(blocks)
}

var (
	totalAllocateInode int64 = 0
)

func (fs *Ext4Filesystem) allocateInode() (*InodeWrapper, error) {
	start := time.Now()

	var inode *InodeWrapper = nil
	var err error

	for _, bg := range fs.bgs {
		inode, err = bg.allocateInode()
		if err != nil {
			return nil, err
		}

		if inode != nil {
			break
		}
	}

	if inode == nil {
		return nil, fmt.Errorf("filesystem has run out of inodes")
	}

	// Add the inode to the main inode index.
	fs.inodes[inode.num] = inode

	// Update the free inode count.
	fs.sb.SetFreeInodesCount(fs.sb.FreeInodesCount() - 1)

	totalAllocateInode += int64(time.Since(start).Nanoseconds())

	return inode, nil
}

func (fs *Ext4Filesystem) root() *InodeWrapper {
	return fs.inodes[2]
}

func (fs *Ext4Filesystem) resolveSymlink(node *InodeWrapper, name string) (*InodeWrapper, error) {
	if node.Mode().Type() == goFs.ModeSymlink {
		target := node.linkTarget

		newTarget, err := resolveRelative(path.Unix.Dir(name), target)
		if err != nil {
			return nil, err
		}

		newNode, err := fs.getNode(newTarget, false, false, false)
		if err != nil {
			return nil, err
		}

		return newNode, nil
	}

	return node, nil
}

func (fs *Ext4Filesystem) getNode(filename string, debug bool, mkdir bool, resolveSymlinks bool) (*InodeWrapper, error) {
	filename = strings.TrimSuffix(filename, "/")

	if inode, ok := fs.inodeCache[filename]; ok {
		if resolveSymlinks {
			return fs.resolveSymlink(inode, filename)
		} else {
			return inode, nil
		}
	}

	parentName := path.Unix.Dir(filename)

	if parent, ok := fs.inodeCache[parentName]; ok {
		// Unconditionally resolve symlinks here.
		var err error
		parent, err = fs.resolveSymlink(parent, parentName)
		if err != nil {
			return nil, err
		}

		child, err := parent.getChild(path.Unix.Base(filename))
		if err == os.ErrNotExist && mkdir {
			token := path.Unix.Base(filename)

			d, err := fs.allocateInode()
			if err != nil {
				return nil, err
			}

			if err := d.allocateDirectory(parent); err != nil {
				return nil, err
			}

			if err := parent.addDirectoryEntry(d, token); err != nil {
				return nil, err
			}

			child, err = parent.getChild(token)
			if err != nil {
				return nil, fmt.Errorf("failed to get child %s: %s", token, err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("failed to get child (cached) %s: %s", path.Unix.Base(filename), err)
		}

		fs.inodeCache[filename] = child

		if resolveSymlinks {
			return fs.resolveSymlink(child, filename)
		} else {
			return child, nil
		}
	}

	tokens := strings.Split(filename, "/")

	if debug {
		log.Default().Debug("", "tokens", tokens)
	}

	currentNode := fs.root()

	for _, token := range tokens[1:] {
		if token == "" {
			continue
		}

		child, err := currentNode.getChild(token)
		if err == os.ErrNotExist && mkdir {
			d, err := fs.allocateInode()
			if err != nil {
				return nil, err
			}

			if err := d.allocateDirectory(currentNode); err != nil {
				return nil, err
			}

			if err := currentNode.addDirectoryEntry(d, token); err != nil {
				return nil, err
			}

			child, err = currentNode.getChild(token)
			if err != nil {
				return nil, fmt.Errorf("failed to get child %s: %s", token, err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("failed to get child %s: %s", token, err)
		}

		if debug {
			log.Default().Debug("", "name", token, "child", child)
		}

		currentNode = child
	}

	fs.inodeCache[filename] = currentNode

	if resolveSymlinks {
		return fs.resolveSymlink(currentNode, filename)
	} else {
		return currentNode, nil
	}
}

func (fs *Ext4Filesystem) mkdir(filename string, all bool) (*InodeWrapper, error) {
	parentName := path.Unix.Dir(filename)
	newDirName := path.Unix.Base(filename)

	// check if it already exists
	if _, err := fs.getNode(filename, false, false, false); err == nil {
		return nil, filesystem.ErrExist{Name: filename}
	}

	node, err := fs.getNode(parentName, false, all, false)
	if err != nil {
		return nil, err
	}

	d, err := fs.allocateInode()
	if err != nil {
		return nil, err
	}

	if err := d.allocateDirectory(node); err != nil {
		return nil, err
	}

	if err := node.addDirectoryEntry(d, path.Unix.Base(newDirName)); err != nil {
		return nil, err
	}

	fs.inodeCache[filename] = d

	return d, nil
}

func (fs *Ext4Filesystem) Mkdir(filename string, all bool) error {
	_, err := fs.mkdir(filename, all)
	return err
}

func (fs *Ext4Filesystem) createFile(filename string, content vm.MemoryRegion) (*InodeWrapper, error) {
	node, err := fs.getNode(path.Unix.Dir(filename), false, false, true)
	if err != nil {
		return nil, err
	}

	if !node.Mode().IsDir() {
		return nil, fmt.Errorf("parent is not a directory: %s", node.Mode().Type())
	}

	f, err := fs.allocateInode()
	if err != nil {
		return nil, fmt.Errorf("failed to allocate inode: %v", err)
	}

	if err := f.addContents(content, false); err != nil {
		return nil, fmt.Errorf("failed to add contents: %v", err)
	}

	if err := node.addDirectoryEntry(f, path.Unix.Base(filename)); err != nil {
		return nil, fmt.Errorf("CreateFile(%s): failed to addDirectoryEntry: %v", filename, err)
	}

	return f, nil
}

func (fs *Ext4Filesystem) CreateFile(filename string, content vm.MemoryRegion) error {
	_, err := fs.createFile(filename, content)
	return err
}

func (fs *Ext4Filesystem) link(filename string, target string) (*InodeWrapper, error) {
	if !strings.HasPrefix(target, "/") {
		return nil, fmt.Errorf("hard links must use absolute paths: %s", target)
	}

	node, err := fs.getNode(path.Unix.Dir(filename), false, false, true)
	if err != nil {
		return nil, err
	}

	if !node.Mode().IsDir() {
		return nil, goFs.ErrInvalid
	}

	targetNode, err := fs.getNode(target, false, false, false)
	if err != nil {
		return nil, err
	}

	if err := node.addDirectoryEntry(targetNode, path.Unix.Base(filename)); err != nil {
		return nil, err
	}

	return targetNode, nil
}

func (fs *Ext4Filesystem) Link(filename string, target string) error {
	_, err := fs.link(filename, target)
	return err
}

func (fs *Ext4Filesystem) symlink(filename string, target string) (*InodeWrapper, error) {
	node, err := fs.getNode(path.Unix.Dir(filename), false, false, true)
	if err != nil {
		return nil, err
	}

	if !node.Mode().IsDir() {
		return nil, goFs.ErrInvalid
	}

	f, err := fs.allocateInode()
	if err != nil {
		return nil, err
	}

	if err := f.addContents(vm.RawRegion(target), true); err != nil {
		return nil, err
	}

	if err := node.addDirectoryEntry(f, path.Unix.Base(filename)); err != nil {
		return nil, err
	}

	return f, nil
}

func (fs *Ext4Filesystem) Symlink(filename string, target string) error {
	_, err := fs.symlink(filename, target)
	return err
}

func (fs *Ext4Filesystem) Exists(filename string) bool {
	_, err := fs.getNode(filename, false, false, false)
	return err == nil
}

func (fs *Ext4Filesystem) Chmod(filename string, mode goFs.FileMode) error {
	node, err := fs.getNode(filename, false, false, false)
	if err != nil {
		return err
	}

	return node.chmod(mode)
}

func (fs *Ext4Filesystem) Chown(filename string, uid uint16, gid uint16) error {
	node, err := fs.getNode(filename, false, false, false)
	if err != nil {
		return err
	}

	return node.chown(uid, gid)
}

func (fs *Ext4Filesystem) Chtimes(filename string, mod time.Time) error {
	node, err := fs.getNode(filename, false, false, false)
	if err != nil {
		return err
	}

	return node.chtime(mod)
}

var (
	totalMapRegion int64 = 0
)

func (fs *Ext4Filesystem) mapRegion(region vm.MemoryRegion, offset int64) error {
	if offset == 0 {
		return fmt.Errorf("mapRegion offset == 0 (null pointer)")
	}

	start := time.Now()

	err := fs.vm.Map(region, offset)
	if err != nil {
		return err
	}

	totalMapRegion += int64(time.Since(start).Nanoseconds())

	return nil
}

func (fs *Ext4Filesystem) mapExtent(region vm.MemoryRegion, extent *Extent) error {
	return fs.mapRawExtent(
		vm.NewOffsetRegion(region, int64(extent.FirstFileBlock)*int64(fs.sb.blockSize())),
		extent,
	)
}

func (fs *Ext4Filesystem) mapRawExtent(region vm.MemoryRegion, extent *Extent) error {
	return fs.mapRegion(
		vm.NewTruncatedRegion(region, int64(extent.Length)*int64(fs.sb.blockSize())),
		int64(extent.StartBlock)*int64(fs.sb.blockSize()),
	)
}

func (fs *Ext4Filesystem) DumpDebug(filename string) {
	ent, err := fs.getNode(filename, true, false, false)
	if err != nil {
		log.Default().Error("file does not exist", "filename", filename)
		return
	}

	log.Default().Info("DumpDebug", "ent", ent)
}

func (fs *Ext4Filesystem) DumpInodeMap(out io.Writer) error {
	for num := 0; num < len(fs.inodes); num++ {
		node, ok := fs.inodes[num]
		if !ok {
			continue
		}

		nodeString := node.String()

		if _, err := fmt.Fprintf(out, "%08d: %s\n", num, nodeString); err != nil {
			return err
		}
	}
	return nil
}

func (fs *Ext4Filesystem) MakeDeterministic(fsUuid uuid.UUID, createTime time.Time) error {
	fs.sb.SetLastcheck(uint32(createTime.Unix()))
	fs.sb.SetMkfsTime(uint32(createTime.Unix()))

	fs.sb.WriteAt(fsUuid[:], 104)

	rootNode := fs.inodes[2]

	rootNode.node.SetCtime(uint32(createTime.Unix()))
	rootNode.node.SetMtime(uint32(createTime.Unix()))
	rootNode.node.SetAtime(uint32(createTime.Unix()))

	lostAndFoundNode := fs.inodes[11]

	lostAndFoundNode.node.SetCtime(uint32(createTime.Unix()))
	lostAndFoundNode.node.SetMtime(uint32(createTime.Unix()))
	lostAndFoundNode.node.SetAtime(uint32(createTime.Unix()))

	fs.deterministicTime = createTime

	return nil
}

type RegionWrapperFunc func(string, vm.MemoryRegion) vm.MemoryRegion

type filesystemCreationContext struct {
	extendedRegionMethods filesystem.ExtendedFileMethods
	deferredFilesystem    []func() error
	regionWrapper         RegionWrapperFunc
	skipDirectories       map[filesystem.Directory]struct{}
}

// Recurse into an filesystem.Directory and put all it's contents into a ext4 filesystem.
func (fs *Ext4Filesystem) addDirectory(ctx *filesystemCreationContext, dir filesystem.Directory, name string) error {
	ents, err := dir.Readdir()
	if err != nil {
		return fmt.Errorf("failed to readdir: %w", err)
	}

	for _, ent := range ents {
		info, err := ent.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat: %w", err)
		}

		name := path.Unix.Join(name, path.Unix.Base(ent.Name))

		skip := false

		var node *InodeWrapper

		switch info.Kind() {
		case filesystem.TypeDirectory:
			node, err = fs.mkdir(name, false)
			if err != nil {
				return fmt.Errorf("failed to mkdir %s: %w", name, err)
			}

			child, ok := ent.File.(filesystem.Directory)
			if !ok {
				return fmt.Errorf("directory does not implement Directory: %T", ent.File)
			}

			if _, ok := ctx.skipDirectories[child]; !ok {
				if err := fs.addDirectory(ctx, child, name); err != nil {
					return err
				}
			}
		case filesystem.TypeLink:
			target, err := filesystem.GetLinkName(ent.File)
			if err != nil {
				return fmt.Errorf("failed to get linkname: %w", err)
			}

			node, err = fs.link(name, target)
			if err != nil {
				ctx.deferredFilesystem = append(ctx.deferredFilesystem, func() error {
					if err := fs.Link(name, target); err != nil {
						log.Default().Error("failed to link", "name", name, "target", target, "err", err)

						// major hack to try and reorder things.
						ctx.deferredFilesystem = append(ctx.deferredFilesystem, func() error {
							if err := fs.Link(name, target); err != nil {
								return fmt.Errorf("failed to link: %w", err)
							}

							if err := fs.Chmod(name, info.Mode()); err != nil {
								return fmt.Errorf("failed to chmod: %w", err)
							}

							uid, gid, err := filesystem.GetUidAndGid(ent.File)
							if err != nil {
								return fmt.Errorf("failed to GetUidAndGid: %w", err)
							}

							if err := fs.Chown(name, uint16(uid), uint16(gid)); err != nil {
								return fmt.Errorf("failed to chown: %w", err)
							}

							return nil
						})

						return nil
					}

					if err := fs.Chmod(name, info.Mode()); err != nil {
						return fmt.Errorf("failed to chmod: %w", err)
					}

					uid, gid, err := filesystem.GetUidAndGid(ent.File)
					if err != nil {
						return fmt.Errorf("failed to GetUidAndGid: %w", err)
					}

					if err := fs.Chown(name, uint16(uid), uint16(gid)); err != nil {
						return fmt.Errorf("failed to chown: %w", err)
					}

					return nil
				})

				skip = true
			}
		case filesystem.TypeSymlink:
			target, err := filesystem.GetLinkName(ent.File)
			if err != nil {
				return fmt.Errorf("failed to get linkname: %w", err)
			}

			node, err = fs.symlink(name, target)
			if err != nil {
				return fmt.Errorf("failed to make symlink: %w", err)
			}
		case filesystem.TypeRegular:
			var region vm.MemoryRegion

			if openRegion, ok := ent.File.(filesystem.HasOpenRegion); ok {
				region, err = openRegion.OpenRegion()
				if err != nil {
					return fmt.Errorf("failed to open region for guest: %T %w", ent.File, err)
				}
			} else {
				f, err := ent.Open()
				if err != nil {
					return fmt.Errorf("failed to open file for guest: %T %w", ent.File, err)
				}

				region = vm.NewReaderRegion(f, info.Size())
			}

			if ctx.regionWrapper != nil {
				region = ctx.regionWrapper(name, region)
			}

			node, err = fs.createFile(name, region)
			if err != nil {
				return fmt.Errorf("failed to create file in guest %s: %w", name, err)
			}
		default:
			return fmt.Errorf("unimplemented kind: %s", info.Kind())
		}

		if !skip {
			if err := node.chmod(info.Mode()); err != nil {
				return fmt.Errorf("failed to chmod: %w", err)
			}

			uid, gid, err := filesystem.GetUidAndGid(ent.File)
			if err != nil {
				return fmt.Errorf("failed to GetUidAndGid: %w", err)
			}

			if err := node.chown(uint16(uid), uint16(gid)); err != nil {
				return fmt.Errorf("failed to chown: %w", err)
			}

			if err := node.chtime(info.ModTime()); err != nil {
				return fmt.Errorf("failed to chtime: %w", err)
			}
		}
	}

	return nil
}

func (fs *Ext4Filesystem) AddDirectory(
	extendedRegionMethods filesystem.ExtendedFileMethods,
	dir filesystem.Directory,
	wrapper RegionWrapperFunc,
	skipDirectories map[filesystem.Directory]struct{},
) error {
	ctx := &filesystemCreationContext{
		extendedRegionMethods: extendedRegionMethods,
		regionWrapper:         wrapper,
		skipDirectories:       skipDirectories,
	}

	if err := fs.addDirectory(ctx, dir, "/"); err != nil {
		return fmt.Errorf("failed to convert filesystem to ext4: %w", err)
	}

	for _, fn := range ctx.deferredFilesystem {
		if err := fn(); err != nil {
			return fmt.Errorf("failed to run deferred filesystem operation: %w", err)
		}
	}

	return nil
}

func (fs *Ext4Filesystem) PrintStats(log log.Handler) {
	log.Info("ext4 stats",
		"totalMapRegion", float64(totalMapRegion)/1000/1000,
		"totalAllocateInode", float64(totalAllocateInode)/1000/1000,
		"totalAddContents", float64(totalAddContents)/1000/1000,
		"totalAllocateBlocks", float64(totalAllocateBlocks)/1000/1000,
	)
}

func roundUpDiv[T constraints.Integer](x, y T) T {
	return 1 + (x-1)/y
}

func (sb *Superblock) blockSize() uint64 {
	return uint64(math.Pow(2, float64(10+sb.LogBlockSize())))
}

func CreateExt4Filesystem(_vm *vm.VirtualMemory, offset int64, size int64) (*Ext4Filesystem, error) {
	inodesPerGroup := 8192
	blockSize := 4096
	blockCount := roundUpDiv(size, int64(blockSize))
	blocksPerGroup := 32768
	blockGroupCount := roundUpDiv(blockCount, int64(blocksPerGroup))
	inodeCount := blockGroupCount * int64(inodesPerGroup)

	log.Default().Debug("making exr4 filesystem", "vmPageSize", _vm.PageSize(), "blocks", blockCount, "inodes", inodeCount, "blockGroups", blockGroupCount)

	fs := &Ext4Filesystem{
		vm:         _vm,
		sb:         &Superblock{},
		inodes:     make(map[int]*InodeWrapper),
		inodeCache: make(map[string]*InodeWrapper),
	}

	// Map the superblock.
	if err := fs.mapRegion(fs.sb, 1024); err != nil {
		return nil, err
	}

	// Initialize the superblock.
	fs.sb.SetMagic(61267)
	fs.sb.SetBlocksCountLo(uint32(blockCount))
	fs.sb.SetBlocksCountHi(uint32(blockCount >> 32))
	fs.sb.SetInodesCount(uint32(inodeCount))
	fs.sb.SetRBlocksCountLo(0)
	fs.sb.SetRBlocksCountHi(0)
	fs.sb.SetLogBlockSize(2)
	fs.sb.SetLogClusterSize(2)
	fs.sb.SetBlocksPerGroup(uint32(blocksPerGroup))
	fs.sb.SetClustersPerGroup(uint32(blocksPerGroup))
	fs.sb.SetInodesPerGroup(uint32(inodesPerGroup))
	fs.sb.SetFreeInodesCount(uint32(inodeCount))
	freeBlocks := uint32(blockCount - 1)
	fs.sb.SetFreeBlocksCountLo(freeBlocks)
	fs.sb.SetFreeBlocksCountHi(0)
	fs.sb.SetMaxMntCount(65535)
	fs.sb.SetLastcheck(uint32(time.Now().Unix()))
	fs.sb.SetMkfsTime(uint32(time.Now().Unix()))
	fs.sb.SetInodeSize(INODE_SIZE)
	fs.sb.SetDescSize(64)
	fs.sb.SetRevLevel(1)
	fs.sb.SetFirstIno(11)
	fs.sb.SetState(1)
	fs.sb.SetErrors(2)
	fs.sb.SetLogGroupsPerFlex(4)
	fs.sb.SetFirstDataBlock(0)

	uuid := uuid.New()

	// Set uuid
	fs.sb.WriteAt(uuid[:], 104)

	fs.sb.SetFeatureCompat(
		uint32(Feature_compat_COMPAT_SPARSE_SUPER2),
	)
	fs.sb.SetFeatureIncompat(
		uint32(Feature_incompat_INCOMPAT_64BIT) |
			uint32(Feature_incompat_INCOMPAT_FILETYPE) |
			uint32(Feature_incompat_INCOMPAT_EXTENTS) |
			uint32(Feature_incompat_INCOMPAT_FLEX_BG),
	)
	fs.sb.SetFeatureRoCompat(
		uint32(Feature_ro_compat_RO_COMPAT_SPARSE_SUPER) |
			uint32(Feature_ro_compat_RO_COMPAT_LARGE_FILE) |
			uint32(Feature_ro_compat_RO_COMPAT_HUGE_FILE),
	)

	// Map and initialize all the block group descriptors.
	var blockGroupOffset int64
	if fs.sb.blockSize() == 1024 {
		return nil, fmt.Errorf("block size of 1024 not implemented")
	} else {
		blockGroupOffset = int64(fs.sb.blockSize())
	}

	for i := 0; i < int(blockGroupCount); i++ {
		inodeBitmapSize := roundUpDiv(uint64(fs.sb.InodesPerGroup())/8, fs.sb.blockSize()) * fs.sb.blockSize() * 8
		blockBitmapSize := roundUpDiv(uint64(fs.sb.BlocksPerGroup())/8, fs.sb.blockSize()) * fs.sb.blockSize() * 8

		bg := &BlockGroup{
			fs:         fs,
			num:        i,
			offset:     blockGroupOffset,
			desc:       &BlockGroupDescriptor{},
			firstBlock: int64(i) * int64(blocksPerGroup),

			inodeBitmap: vm.NewBitmap(inodeBitmapSize),
			blockBitmap: vm.NewBitmap(blockBitmapSize),

			inodeCount: uint32(min(inodeCount, int64(fs.sb.InodesPerGroup()))),
			blockCount: uint32(min(blockCount, int64(fs.sb.BlocksPerGroup()))),
		}

		// Set padding bits for inodes (from actual count to bitmap size) - bulk operation
		if bg.inodeCount < uint32(inodeBitmapSize) {
			if err := bg.inodeBitmap.SetRange(uint64(bg.inodeCount), uint64(inodeBitmapSize), true); err != nil {
				return nil, err
			}
		}

		// Set padding bits for blocks (from actual count to bitmap size)
		// This is the critical fix - we need to handle the last block group specially
		actualBlockCount := bg.blockCount
		if i == int(blockGroupCount)-1 {
			// For the last block group, calculate actual blocks
			totalBlocksInPreviousGroups := int64(i) * int64(blocksPerGroup)
			actualBlockCount = uint32(blockCount - totalBlocksInPreviousGroups)
		}

		// Set all bits from actualBlockCount to the end of the bitmap - bulk operation
		if actualBlockCount < uint32(blockBitmapSize) {
			if err := bg.blockBitmap.SetRange(uint64(actualBlockCount), uint64(blockBitmapSize), true); err != nil {
				return nil, err
			}
		}

		// Update the block count to reflect actual blocks in this group
		bg.blockCount = actualBlockCount

		bg.desc.SetFreeBlocksCountLo(uint16(bg.blockCount))
		bg.desc.SetFreeBlocksCountHi(uint16(bg.blockCount >> 16))
		bg.desc.SetFreeInodesCountLo(uint16(bg.inodeCount))
		bg.desc.SetFreeInodesCountHi(uint16(bg.inodeCount >> 16))
		bg.desc.SetFlags(4)

		fs.bgs = append(fs.bgs, bg)

		if i == 0 {
			if fs.sb.blockSize() == 1024 {
				return nil, fmt.Errorf("block size of 1024 not implemented")
			} else {
				// Allocate a single block (which will be at the start) for the super block.
				// Ignore errors since we expect this to be a null pointer error.
				fs.allocateBlocks(1)

				_, err := fs.allocateBlocksForBytes(blockGroupCount * 64) // BlockGroupDescriptor is 64 bytes
				if err != nil {
					return nil, fmt.Errorf("could not allocate bgd blocks: %v", err)
				}
			}
		}

		if err := fs.mapRegion(bg.desc, blockGroupOffset); err != nil {
			return nil, fmt.Errorf("failed to reinterpret block group: %v", err)
		}
		blockGroupOffset += bg.desc.Size()

		var extent *Extent
		var err error

		// map the inode bitmap and block bitmap.
		extent, err = fs.allocateBlocksForBytes(bg.blockBitmap.Size())
		if err != nil {
			return nil, err
		}
		bg.desc.SetBlockBitmapLo(uint32(extent.StartBlock))
		bg.desc.SetBlockBitmapHi(uint32(extent.StartBlock >> 32))
		if err := fs.mapExtent(bg.blockBitmap, extent); err != nil {
			return nil, err
		}

		extent, err = fs.allocateBlocksForBytes(bg.inodeBitmap.Size())
		if err != nil {
			return nil, err
		}
		bg.desc.SetInodeBitmapLo(uint32(extent.StartBlock))
		bg.desc.SetInodeBitmapHi(uint32(extent.StartBlock >> 32))
		if err := fs.mapExtent(bg.inodeBitmap, extent); err != nil {
			return nil, err
		}

		// map the inode table.
		extent, err = fs.allocateBlocksForBytes(int64(fs.sb.InodeSize()) * int64(bg.inodeCount))
		if err != nil {
			return nil, err
		}
		bg.desc.SetInodeTableLo(uint32(extent.StartBlock))
		bg.desc.SetInodeTableHi(uint32(extent.StartBlock >> 32))

		// log.Default().Info("", "block bitmap", bg.desc.blockBitmapBlock(), "inode bitmap", bg.desc.inodeBitmapBlock(), "inode table", bg.desc.inodeTableBlock())
	}

	// Create the set of default inodes and the root directory.
	var root *InodeWrapper

	for range 11 {
		inode, err := fs.allocateInode()
		if err != nil {
			return nil, err
		}

		switch inode.num {
		case 2: // root directory.
			err := inode.allocateDirectory(inode)
			if err != nil {
				return nil, err
			}

			root = inode

			if err := root.chmod(goFs.FileMode(0755)); err != nil {
				return nil, err
			}
		case 11: // lost + found directory
			err := inode.allocateDirectory(root)
			if err != nil {
				return nil, err
			}

			if err := inode.chmod(goFs.FileMode(0700)); err != nil {
				return nil, err
			}

			if err := root.addDirectoryEntry(inode, "lost+found"); err != nil {
				return nil, err
			}
		default:
			inode.node.SetMode(0)
		}
	}

	// Map a page at the very end of the storage to set the size.
	if err := fs.mapRegion(make(vm.RawRegion, 1), size-1); err != nil {
		return nil, err
	}

	return fs, nil
}
