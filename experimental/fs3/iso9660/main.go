package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/tinyrange/tinyrange/experimental/fs3"
	"github.com/tinyrange/tinyrange/pkg/log"
)

func (i Int32LsbMsb) Get() int32 {
	return i.GetLsb()
}

func (i *Int32LsbMsb) Set(v int32) {
	i.SetLsb(v)
	i.SetMsb(v)
}

func (i Int16LsbMsb) Get() int16 {
	return i.GetLsb()
}

func (i *Int16LsbMsb) Set(v int16) {
	i.SetLsb(v)
	i.SetMsb(v)
}

type DirectoryEntry interface {
	GetBase() BaseDirectoryEntry
	SetBase(v BaseDirectoryEntry)

	GetName() string
	SetName(v string)
}

func (r RootDirectoryEntry) GetName() string {
	return ""
}

func (r *RootDirectoryEntry) SetName(v string) {
}

func (d *DynDirectoryEntry) GetName() string {
	return string(d.GetFilename()[:d.GetFilenameLength()])
}

func (d *DynDirectoryEntry) SetName(v string) {
	d.SetFilename([]byte(v))
	d.SetFilenameLength(uint8(len(v)))

	base := d.GetBase()
	base.SetSize(uint8(base.Size()) + 1 + uint8(d.GetFilenameLength()))
	d.SetBase(base)
}

var (
	_ DirectoryEntry = &RootDirectoryEntry{}
	_ DirectoryEntry = &DynDirectoryEntry{}
)

type iso9660Node struct {
	id  int64
	ent DirectoryEntry
}

// ID implements fs3.Node.
func (i *iso9660Node) ID() int64 {
	return i.id
}

// Kind implements fs3.Node.
func (i *iso9660Node) Kind() fs3.NodeKind {
	if i.ent.GetBase().GetFlags().Has(DirectoryEntryFlagsDirectory) {
		return fs3.TypeDirectory
	} else {
		return fs3.TypeRegular
	}
}

// Size implements fs3.Node.
func (i *iso9660Node) Size() int64 {
	return int64(i.ent.GetBase().GetDataLength().Get())
}

var (
	_ fs3.Node = &iso9660Node{}
)

type iso9660DirectoryEntry struct {
	*iso9660Node
	name string
}

// Name implements fs3.DirectoryEntry.
func (i *iso9660DirectoryEntry) Name() string {
	return i.name
}

var (
	_ fs3.DirectoryEntry = &iso9660DirectoryEntry{}
)

type iso9660Reader struct {
	reader  io.ReaderAt
	primary *PrimaryVolumeDescriptor
}

func (i *iso9660Reader) logicalBlockSize() int32 {
	return int32(i.primary.GetLogicalBlockSize().Get())
}

func (i *iso9660Reader) readDirectory(ent DirectoryEntry, f func(id int64, ent DirectoryEntry) error) error {
	blockSize := i.logicalBlockSize()

	base := ent.GetBase()

	data := make([]byte, base.GetDataLength().Get())

	extent := base.GetExtentLocation().Get()
	off := int64(extent * blockSize)

	blocks := len(data) / int(blockSize)

	if _, err := i.reader.ReadAt(data, off); err != nil {
		return fmt.Errorf("failed to read directory data: %w", err)
	}

	for i := 0; i < blocks; i++ {
		start := i * int(blockSize)
		blockData := data[start : start+int(blockSize)]
		localOffset := off + int64(start)

		for len(blockData) > 0 {
			full := DynDirectoryEntry(blockData)

			dataLength := full.GetBase().GetSize()

			if dataLength == 0 {
				break
			}

			ent := DynDirectoryEntry(full[:dataLength])

			blockData = blockData[dataLength:]
			localOffset += int64(dataLength)

			if ent.GetBase().GetExtendedAttributeRecordLength() != 0 {
				slog.Warn("extended attributes are not supported")
			}

			name := ent.GetName()

			// skip the current and parent directory entries
			if name == "\x00" || name == "\x01" {
				continue
			}

			if err := f(localOffset, &ent); err != nil {
				return fmt.Errorf("failed to iterate directory entry: %w", err)
			}
		}
	}

	return nil
}

// IterateNodes implements fs3.FilesystemReader.
func (i *iso9660Reader) IterateNodes() fs3.NodeIterator {
	// The nodes that are returned are extended directory entries.
	// The basic idea is we iterate through the path table and then through each directory.
	return fs3.NewNodeIterator(func(it fs3.NodeIteratorImpl) error {
		var (
			iterateEntry     func(id int64, ent DirectoryEntry) error
			iterateDirectory func(ent DirectoryEntry) error
		)

		iterateEntry = func(id int64, ent DirectoryEntry) error {
			base := ent.GetBase()

			if base.GetExtendedAttributeRecordLength() != 0 {
				slog.Warn("extended attributes are not supported")
			}

			if base.GetFlags().Has(DirectoryEntryFlagsDirectory) {
				it.MaybeEmit(&iso9660Node{
					id:  id,
					ent: ent,
				})

				if err := iterateDirectory(ent); err != nil {
					return fmt.Errorf("failed to iterate directory: %w", err)
				}
			} else {
				it.MaybeEmit(&iso9660Node{
					id:  id,
					ent: ent,
				})
			}

			return nil
		}

		iterateDirectory = func(ent DirectoryEntry) error {
			return i.readDirectory(ent, func(id int64, ent DirectoryEntry) error {
				return iterateEntry(id, ent)
			})
		}

		root := i.primary.GetRootDirectoryEntry()

		if err := iterateEntry(0, &root); err != nil {
			return fmt.Errorf("failed to iterate root directory: %w", err)
		}

		return nil
	})
}

// RootNode implements fs3.FilesystemReader.
func (i *iso9660Reader) RootNode() (fs3.Node, error) {
	rootEnt := i.primary.GetRootDirectoryEntry()

	return &iso9660Node{
		id:  0,
		ent: &rootEnt,
	}, nil
}

type iso9660ReaderHandle struct {
	reader  *iso9660Reader
	isoNode *iso9660Node
}

// ReadAt implements fs3.ReaderHandle.
func (i *iso9660ReaderHandle) ReadAt(p []byte, off int64) (n int, err error) {
	base := i.isoNode.ent.GetBase()

	blockSize := i.reader.logicalBlockSize()

	baseOffset := off

	// Clip the read to the file size.
	if baseOffset >= i.isoNode.Size() {
		return 0, io.EOF
	}

	if baseOffset+int64(len(p)) > i.isoNode.Size() {
		p = p[:i.isoNode.Size()-baseOffset]
	}

	extent := base.GetExtentLocation().Get()
	off += int64(extent * blockSize)

	return i.reader.reader.ReadAt(p, off)
}

var (
	_ fs3.ReaderHandle = &iso9660ReaderHandle{}
)

// ReadContents implements fs3.FilesystemReader.
func (i *iso9660Reader) OpenContents(node fs3.Node) (fs3.ReaderHandle, error) {
	isoNode, ok := node.(*iso9660Node)
	if !ok {
		return nil, fmt.Errorf("unexpected node type: %T", node)
	}

	if isoNode.Kind() != fs3.TypeRegular {
		return nil, fmt.Errorf("node is not a regular file")
	}

	return &iso9660ReaderHandle{
		reader:  i,
		isoNode: isoNode,
	}, nil
}

// ReadDirectory implements fs3.FilesystemReader.
func (i *iso9660Reader) IterateDirectory(node fs3.Node) (fs3.DirectoryIterator, error) {
	isoNode, ok := node.(*iso9660Node)
	if !ok {
		return nil, fmt.Errorf("unexpected node type: %T", node)
	}

	if isoNode.Kind() != fs3.TypeDirectory {
		return nil, fmt.Errorf("node is not a directory")
	}

	return fs3.NewDirectoryIterator(func(it fs3.DirectoryIteratorImpl) error {
		return i.readDirectory(isoNode.ent, func(id int64, ent DirectoryEntry) error {
			it.MaybeEmit(&iso9660DirectoryEntry{
				iso9660Node: &iso9660Node{
					id:  id,
					ent: ent,
				},
				name: ent.GetName(),
			})

			return nil
		})
	}), nil
}

var (
	_ fs3.FilesystemReader = &iso9660Reader{}
)

func OpenIso9660Image(reader io.ReaderAt) (fs3.FilesystemReader, error) {
	var ret iso9660Reader
	ret.reader = reader

	// Parse each volume descriptor.
	var off int64 = 0x8000
outer:
	for {
		var volumeDescriptor VolumeDescriptor
		if _, err := reader.ReadAt(volumeDescriptor[:], off); err != nil {
			return nil, fmt.Errorf("failed to read volume descriptor: %w", err)
		}

		// Check the identifier.
		id := volumeDescriptor.IdentifierSlice()
		if !bytes.Equal(id[:], []byte("CD001")) {
			return nil, fmt.Errorf("unexpected volume descriptor identifier: %v", id)
		}

		switch volumeDescriptor.GetKind() {
		case VolumeDescriptorKindPrimary:
			primary := volumeDescriptor.GetContents().GetPrimaryVolumeDescriptor()

			ret.primary = &primary
		case VolumeDescriptorKindBootRecord:
			boot := volumeDescriptor.GetContents().GetBootRecord()

			_ = boot
		case VolumeDescriptorKindTerminator:
			break outer
		default:
			return nil, fmt.Errorf("unexpected volume descriptor kind: %v", volumeDescriptor.GetKind())
		}

		off += volumeDescriptor.Size()
	}

	return &ret, nil
}

var (
	inputFilename = flag.String("input", "", "the iso9660 image to read")
)

func appMain() error {
	flag.Parse()

	if *inputFilename == "" {
		return fmt.Errorf("input filename is required")
	}

	file, err := os.Open(*inputFilename)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer file.Close()

	reader, err := OpenIso9660Image(file)
	if err != nil {
		return fmt.Errorf("failed to open iso9660 image: %w", err)
	}

	it := reader.IterateNodes()

	for node := range it.Chan() {
		switch node.Kind() {
		case fs3.TypeRegular:
			fmt.Printf("regular: %v %d\n", node.ID(), node.Size())
			fh, err := reader.OpenContents(node)
			if err != nil {
				return fmt.Errorf("failed to read contents: %w", err)
			}

			reader := io.NewSectionReader(fh, 0, node.Size())

			hash := sha256.New()
			if _, err := io.Copy(hash, reader); err != nil {
				return fmt.Errorf("failed to hash contents: %w", err)
			}

			fmt.Printf("hash: %x\n", hash.Sum(nil))
		case fs3.TypeDirectory:
			fmt.Printf("directory: %v\n", node.ID())

			dirIt, err := reader.IterateDirectory(node)
			if err != nil {
				return fmt.Errorf("failed to read directory: %w", err)
			}

			for dir := range dirIt.Chan() {
				fmt.Printf("entry: %v\n", dir.Name())
			}

			if err := dirIt.Err(); err != nil {
				return fmt.Errorf("error during directory iteration: %w", err)
			}
		default:
			return fmt.Errorf("unexpected node kind: %v", node.Kind())
		}
	}

	if err := it.Err(); err != nil {
		return fmt.Errorf("error during iteration: %w", err)
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}
