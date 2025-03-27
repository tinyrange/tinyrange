package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
	"github.com/tinyrange/tinyrange/pkg/log"
)

type fileBlockAddress int64
type virtualBlockAddress int64

type UnderlyingFile interface {
	io.ReaderAt
	io.WriterAt
	Stat() (fs.FileInfo, error)
}

const BLOCK_SIZE = 4096

const MAX_VIRTUAL_BLOCK = 4 * 1024 * 1024 * 1024

const TRDF_MAGIC = "TRDF"
const TRDF_VERSION = 1

type superblock [4096]byte

func (s *superblock) create() {
	copy(s[:4], TRDF_MAGIC)
	binary.LittleEndian.PutUint32(s[4:8], TRDF_VERSION)
}

func (s *superblock) validate() error {
	if string(s[:4]) != TRDF_MAGIC {
		return fmt.Errorf("invalid magic: %s", s[:4])
	}

	if binary.LittleEndian.Uint32(s[4:8]) != TRDF_VERSION {
		return fmt.Errorf("invalid version: %d", s[4])
	}

	return nil
}

func (s *superblock) setDiskSize(size int64) {
	binary.LittleEndian.PutUint64(s[8:16], uint64(size))
}

func (s *superblock) diskSize() int64 {
	return int64(binary.LittleEndian.Uint64(s[8:16]))
}

func (s *superblock) getRootBlock(index int) fileBlockAddress {
	return fileBlockAddress(binary.LittleEndian.Uint32(s[16+index*4 : 16+(index+1)*4]))
}

func (s *superblock) setRootBlock(index int, block fileBlockAddress) {
	binary.LittleEndian.PutUint32(s[16+index*4:16+(index+1)*4], uint32(block))
}

func (s *superblock) getMetadata() []byte {
	return s[34 : 34+binary.LittleEndian.Uint16(s[32:34])]
}

func (s *superblock) setMetadata(metadata []byte) {
	binary.LittleEndian.PutUint16(s[32:34], uint16(len(metadata)))
	copy(s[34:], metadata)
}

type indirectBlock struct {
	offset fileBlockAddress
	values [4096]byte
}

func (i *indirectBlock) get(index int) fileBlockAddress {
	return fileBlockAddress(binary.LittleEndian.Uint32(i.values[index*4 : (index+1)*4]))
}

func (i *indirectBlock) set(index int, block fileBlockAddress) {
	binary.LittleEndian.PutUint32(i.values[index*4:(index+1)*4], uint32(block))
}

/*
TinyRangeDiskFormat is a simple disk format that is backed by a single file.
It is designed to sparsely allocate blocks, and to be able to resize the disk
on the fly.

# Layout

The file is organized into blocks of 4096 bytes. The first block is the superblock,
which contains metadata about the disk. The rest of the blocks are data blocks.

Data blocks come in two kinds: data blocks and indirect blocks. Data blocks contain
4096 bytes of data. Indirect blocks contain 1024 block addresses, each of which
points to a data block.

The maximum size of the disk is 16TB (4096 * 4 * 1024 * 1024 * 1024) so there are 4
levels of indirection. The first level of indirection is the superblock.

# Header

The header is 4096 bytes long. It contains the following fields:

- [ 0] Magic: 4 bytes, "TRDF"

- [ 4] Version: 4 bytes, 1

- [ 8] DiskSize: 8 bytes, the current size of the disk in bytes

- [16] RootBlocks 4 * 4 bytes, the block addresses of the root blocks

- [32] MetadataLength 2 bytes, the length of the metadata section in bytes

- [34] Metadata: variable length, the metadata section

# Indirect Blocks

Each indirect block contains 1024 block addresses. Each block address is a single 32-bit
little endian unsigned integer.
*/
type TinyRangeDiskFormat struct {
	// The underlying file that backs the disk.
	file UnderlyingFile

	// The superblock of the disk.
	superBlock superblock

	// Whether the superblock is dirty and needs to be written to disk.
	superblockDirty bool

	// The last block written to the disk.
	lastBlockWritten fileBlockAddress

	// A cache of indirect blocks.
	blockCache map[fileBlockAddress]*indirectBlock

	// A list of pending writes. These are writes that have been made to the cache
	// but not yet written to the file.
	pendingWrites map[fileBlockAddress]struct{}
}

func (t *TinyRangeDiskFormat) offsetToVirtualBlockAddress(offset int64) (virtualBlockAddress, int64) {
	return virtualBlockAddress(offset / BLOCK_SIZE), offset % BLOCK_SIZE
}

func (t *TinyRangeDiskFormat) underlyingSize() (int64, error) {
	fi, err := t.file.Stat()
	if err != nil {
		return 0, fmt.Errorf("underlyingSize: %w", err)
	}

	return fi.Size(), nil
}

func (t *TinyRangeDiskFormat) readDirectBlock(p []byte, block fileBlockAddress) error {
	n, err := t.file.ReadAt(p, int64(block)*BLOCK_SIZE)
	if err == io.EOF {
		return err
	} else if err != nil {
		return fmt.Errorf("readDirectBlock(%d): %w", block, err)
	} else if n != len(p) {
		return fmt.Errorf("short read: %d", n)
	}

	return nil
}

func (t *TinyRangeDiskFormat) writeDirectBlock(p []byte, block fileBlockAddress) error {
	n, err := t.file.WriteAt(p, int64(block)*BLOCK_SIZE)
	if err == io.EOF {
		return err
	} else if err != nil {
		return fmt.Errorf("writeDirectBlock(%d): %w", block, err)
	} else if n != len(p) {
		return fmt.Errorf("short write: %d", n)
	}

	return nil
}

func (t *TinyRangeDiskFormat) readIndirectFromCache(block fileBlockAddress) (*indirectBlock, error) {
	if p, ok := t.blockCache[block]; ok {
		return p, nil
	}

	var p indirectBlock

	p.offset = block

	err := t.readDirectBlock(p.values[:], block)
	if err == io.EOF {
		if err := t.writeDirectBlock(p.values[:], block); err != nil {
			return nil, fmt.Errorf("readIndirectFromCache(%d) write new block: %w", block, err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("readIndirectFromCache(%d) read block: %w", block, err)
	}

	t.blockCache[block] = &p

	return &p, nil
}

func (t *TinyRangeDiskFormat) writeIndirectToCache(p *indirectBlock) {
	t.blockCache[p.offset] = p
	t.pendingWrites[p.offset] = struct{}{}
}

func (t *TinyRangeDiskFormat) allocateBlock() (fileBlockAddress, error) {
	block := t.lastBlockWritten + 1
	t.lastBlockWritten = block

	return block, nil
}

func (t *TinyRangeDiskFormat) virtualToIndirectOffsets(block virtualBlockAddress) (first int, second int, third int, fourth int) {
	first = int(block / (1024 * 1024 * 1024))
	second = int((block % (1024 * 1024 * 1024)) / (1024 * 1024))
	third = int((block % (1024 * 1024)) / 1024)
	fourth = int(block % 1024)

	return
}

func (t *TinyRangeDiskFormat) virtualToFileBlockAddress(block virtualBlockAddress) (fileBlockAddress, error) {
	if block > MAX_VIRTUAL_BLOCK {
		return fileBlockAddress(0), fmt.Errorf("virtualToFileBlockAddress(%d): block out of range", block)
	}

	var err error

	firstIndex, secondIndex, thirdIndex, fourthIndex := t.virtualToIndirectOffsets(block)

	// first level of indirection
	firstBlock := t.superBlock.getRootBlock(firstIndex)
	if firstBlock == 0 {
		firstBlock, err = t.allocateBlock()
		if err != nil {
			return fileBlockAddress(0), fmt.Errorf("virtualToFileBlockAddress(%d) first: %w", block, err)
		}
		t.superBlock.setRootBlock(firstIndex, firstBlock)
		t.superblockDirty = true
	}

	first, err := t.readIndirectFromCache(firstBlock)
	if err != nil {
		return fileBlockAddress(0), fmt.Errorf("virtualToFileBlockAddress(%d) first: %w", block, err)
	}

	// second level of indirection
	secondBlock := first.get(secondIndex)
	if secondBlock == 0 {
		secondBlock, err = t.allocateBlock()
		if err != nil {
			return fileBlockAddress(0), fmt.Errorf("virtualToFileBlockAddress(%d) second: %w", block, err)
		}
		first.set(secondIndex, secondBlock)
		t.writeIndirectToCache(first)
	}

	second, err := t.readIndirectFromCache(secondBlock)
	if err != nil {
		return fileBlockAddress(0), fmt.Errorf("virtualToFileBlockAddress(%d) second: %w", block, err)
	}

	// third level of indirection
	thirdBlock := second.get(thirdIndex)
	if thirdBlock == 0 {
		thirdBlock, err = t.allocateBlock()
		if err != nil {
			return fileBlockAddress(0), fmt.Errorf("virtualToFileBlockAddress(%d) third: %w", block, err)
		}
		second.set(thirdIndex, thirdBlock)
		t.writeIndirectToCache(second)
	}

	third, err := t.readIndirectFromCache(thirdBlock)
	if err != nil {
		return fileBlockAddress(0), fmt.Errorf("virtualToFileBlockAddress(%d) third: %w", block, err)
	}

	// fourth level of indirection
	fourthBlock := third.get(fourthIndex)
	if fourthBlock == 0 {
		fourthBlock, err = t.allocateBlock()
		if err != nil {
			return fileBlockAddress(0), fmt.Errorf("virtualToFileBlockAddress(%d) fourth: %w", block, err)
		}
		third.set(fourthIndex, fourthBlock)
	}

	return fourthBlock, nil
}

func (t *TinyRangeDiskFormat) readBlock(p []byte, block virtualBlockAddress) error {
	if len(p) != BLOCK_SIZE {
		return fmt.Errorf("readBlock(%d): invalid buffer size %d", block, len(p))
	}

	fileAddr, err := t.virtualToFileBlockAddress(block)
	if err != nil {
		return fmt.Errorf("readBlock(%d): %w", block, err)
	}

	if err := t.readDirectBlock(p, fileAddr); err != nil {
		return fmt.Errorf("readBlock(%d): %w", block, err)
	}

	return nil
}

func (t *TinyRangeDiskFormat) writeBlock(p []byte, block virtualBlockAddress) error {
	if len(p) != BLOCK_SIZE {
		return fmt.Errorf("writeBlock(%d): invalid buffer size %d", block, len(p))
	}

	fileAddr, err := t.virtualToFileBlockAddress(block)
	if err != nil {
		return fmt.Errorf("writeBlock(%d): %w", block, err)
	}

	if err := t.writeDirectBlock(p, fileAddr); err != nil {
		return fmt.Errorf("writeBlock(%d): %w", block, err)
	}

	return nil
}

// ReadAt implements vm.MemoryRegion.
func (t *TinyRangeDiskFormat) ReadAt(p []byte, off int64) (n int, err error) {
	start, startOffset := t.offsetToVirtualBlockAddress(off)
	end, endOffset := t.offsetToVirtualBlockAddress(off + int64(len(p)))

	// split the read into a series of block reads.

	if start == end-1 && startOffset == 0 && endOffset == 0 {
		// The read is contained within a single block.
		return len(p), t.readBlock(p, start)
	}

	log.Error("ReadAt",
		"start", start, "startOffset", startOffset,
		"end", end, "endOffset", endOffset,
	)

	_ = endOffset

	return -1, fmt.Errorf("ReadAt(%d, %d) not implemented", len(p), off)
}

// WriteAt implements vm.MemoryRegion.
func (t *TinyRangeDiskFormat) WriteAt(p []byte, off int64) (n int, err error) {
	start, startOffset := t.offsetToVirtualBlockAddress(off)
	end, endOffset := t.offsetToVirtualBlockAddress(off + int64(len(p)))

	// split the write into a series of block writes.

	if start == end-1 && startOffset == 0 && endOffset == 0 {
		// The write is contained within a single block.
		return len(p), t.writeBlock(p, start)
	}

	log.Error("WriteAt",
		"start", start, "startOffset", startOffset,
		"end", end, "endOffset", endOffset,
	)

	_ = endOffset

	return -1, fmt.Errorf("WriteAt(%d, %d) not implemented", len(p), off)
}

// Size implements vm.MemoryRegion.
func (t *TinyRangeDiskFormat) Size() int64 {
	return t.superBlock.diskSize()
}

func (t *TinyRangeDiskFormat) Resize(size int64) error {
	return fmt.Errorf("Resize(%d) not implemented", size)
}

func (t *TinyRangeDiskFormat) Flush() error {
	// Write the superblock if it is dirty.
	if t.superblockDirty {
		if err := t.writeDirectBlock(t.superBlock[:], 0); err != nil {
			return fmt.Errorf("Flush superblock: %w", err)
		}

		t.superblockDirty = false
	}

	// Write all pending writes.
	for block := range t.pendingWrites {
		indirect := t.blockCache[block]
		if err := t.writeDirectBlock(indirect.values[:], block); err != nil {
			return fmt.Errorf("Flush write block %d: %w", block, err)
		}
	}

	// Clear the pending writes.
	t.pendingWrites = make(map[fileBlockAddress]struct{})

	return nil
}

var (
	_ vm.MemoryRegion = &TinyRangeDiskFormat{}
)

func OpenDisk(file UnderlyingFile, size int64) (*TinyRangeDiskFormat, error) {
	f := &TinyRangeDiskFormat{
		file:          file,
		blockCache:    make(map[fileBlockAddress]*indirectBlock),
		pendingWrites: make(map[fileBlockAddress]struct{}),
	}

	// Read the superblock.
	err := f.readDirectBlock(f.superBlock[:], 0)
	if err == io.EOF {
		// The disk is empty, so we need to create a new superblock.
		f.superBlock.create()
		f.superBlock.setDiskSize(size)

		// Write the superblock.
		if err := f.writeDirectBlock(f.superBlock[:], 0); err != nil {
			return nil, fmt.Errorf("OpenDisk write superblock: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("OpenDisk read superblock: %w", err)
	}

	// Validate the superblock.
	if err := f.superBlock.validate(); err != nil {
		return nil, fmt.Errorf("OpenDisk validate superblock: %w", err)
	}

	// Resize the disk if necessary.
	if f.superBlock.diskSize() != size {
		if err := f.Resize(size); err != nil {
			return nil, fmt.Errorf("OpenDisk resize: %w", err)
		}
	}

	// Get the underlying size.
	underlyingSize, err := f.underlyingSize()
	if err != nil {
		return nil, fmt.Errorf("OpenDisk underlyingSize: %w", err)
	}
	f.lastBlockWritten = fileBlockAddress(underlyingSize / BLOCK_SIZE)

	return f, nil
}

func appMain() error {
	start := time.Now()
	vmem := vm.NewVirtualMemory(512*1024*1024*1024, 4096)
	log.Info("created virtual memory", "duration", time.Since(start))

	start = time.Now()
	fs, err := ext4.CreateExt4Filesystem(vmem, 0, vmem.Size())
	if err != nil {
		return err
	}
	log.Info("created ext4 filesystem", "duration", time.Since(start))

	_ = fs

	f, err := os.Create("local/disk.img")
	if err != nil {
		return err
	}

	disk, err := OpenDisk(f, 512*1024*1024*1024)
	if err != nil {
		return err
	}

	start = time.Now()
	if _, err := vmem.WriteSparseTo(disk); err != nil {
		return err
	}
	log.Info("wrote virtual memory to disk", "duration", time.Since(start))

	start = time.Now()
	if err := disk.Flush(); err != nil {
		return err
	}
	log.Info("flushed disk", "duration", time.Since(start))

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}
