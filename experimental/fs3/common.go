package fs3

import (
	"io"
	"io/fs"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
)

type ReaderHandle interface {
	io.Reader
	io.ReaderAt
}

type NodeKind int

const (
	TypeInvalid NodeKind = iota
	TypeRegular
	TypeDirectory
)

type Node interface {
	// The kind of node.
	Kind() NodeKind
	// A unique identifier for the node.
	// This could be a inode number of a offset on disk.
	ID() int64
	// The size of the node in bytes.
	Size() int64
}

type HasModTime interface {
	Node

	// Returns the last modified time of the node.
	ModTime() time.Time
}

type HasMode interface {
	Node

	// Returns the mode of the node.
	Mode() fs.FileMode
}

type HasOwner interface {
	Node

	// Returns the user id of the node.
	Uid() int
	// Returns the group id of the node.
	Gid() int
}

type DirectoryEntry interface {
	Node
	// Returns the name of the directory entry.
	// Note: This should be the base name of the file, not the full path.
	Name() string
}

type NodeIterator interface {
	// Returns the next node in the filesystem or false if there are no more nodes.
	// The order of nodes is not guaranteed.
	// Next is guaranteed to return all nodes in the filesystem exactly once.
	Next() (Node, bool)
	// Returns an error if one occurred during iteration.
	Error() error
}

type DirectoryEntryIterator interface {
	// Returns the next directory entry in the directory or false if there are no more entries.
	Next() (DirectoryEntry, bool)
	// Returns an error if one occurred during iteration.
	Error() error
}

type FilesystemReader interface {
	// Iterates through all nodes in the filesystem.
	IterateNodes() NodeIterator
	// Returns a `io.Reader`+`io.ReaderAt` for a given inode.
	ReadContents(node Node) (ReaderHandle, error)
	// Returns a list of file entries in a directory.
	ReadDirectory(node Node) (DirectoryEntryIterator, error)
}

type FilesystemWriter interface {
	// Allocate a new Inode on the filesystem. The node has metadata created at this time including modified time, permissions, ownership, etc...
	AllocateNode(node Node) error
	// Set the contents of a file to a memory region.
	WriteContents(node Node, region vm.MemoryRegion) error
	// Write a list of pointers to a existing node turning that node into a directory.
	WriteDirectory(node Node, entries []DirectoryEntry) error
	// Writes final filesystem metadata and closes the filesystem.
	Finalize() error
}
