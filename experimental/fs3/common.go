package fs3

import (
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
)

type ReaderHandle interface {
	io.ReaderAt
}

type NodeKind int

const (
	TypeInvalid NodeKind = iota
	TypeRegular
	TypeDirectory
)

func (n NodeKind) String() string {
	switch n {
	case TypeInvalid:
		return "invalid"
	case TypeRegular:
		return "regular"
	case TypeDirectory:
		return "directory"
	default:
		return "unknown"
	}
}

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

type NodeIteratorImpl interface {
	// Emits a node to the iterator.
	// The node won't be emitted if it has already been emitted (identified by the Id()).
	// Returns true if the node was emitted, false otherwise.
	MaybeEmit(Node) (bool, error)
}

type NodeIterator interface {
	// Returns a channel that emits nodes in the filesystem.
	// The order of nodes is not guaranteed.
	// Next is guaranteed to return all nodes in the filesystem exactly once.
	Chan() <-chan Node
	// Returns an error if one occurred during iteration.
	Err() error
	// Closes the iterator and releases resources.
	Close() error
}

type nodeIteratorImpl struct {
	mtx          sync.Mutex
	c            chan Node
	err          error
	emittedNodes map[int64]struct{}
}

func (n *nodeIteratorImpl) Chan() <-chan Node {
	return n.c
}

func (n *nodeIteratorImpl) MaybeEmit(node Node) (bool, error) {
	if _, ok := n.emittedNodes[node.ID()]; ok {
		return false, nil
	}

	n.emittedNodes[node.ID()] = struct{}{}
	n.c <- node
	if n.err != nil {
		return false, n.err
	}

	return true, nil
}

// Close implements NodeIterator.
func (n *nodeIteratorImpl) Close() error {
	n.mtx.Lock()
	defer n.mtx.Unlock()

	if n.err != nil {
		return n.err
	}

	n.err = io.EOF
	return nil
}

// Err implements NodeIterator.
func (n *nodeIteratorImpl) Err() error {
	return n.err
}

var (
	_ NodeIteratorImpl = &nodeIteratorImpl{}
	_ NodeIterator     = &nodeIteratorImpl{}
)

func NewNodeIterator(f func(NodeIteratorImpl) error) NodeIterator {
	impl := &nodeIteratorImpl{
		c:            make(chan Node),
		emittedNodes: make(map[int64]struct{}),
	}

	go func() {
		defer close(impl.c)

		if err := f(impl); err != nil && impl.err != io.EOF {
			impl.err = err
		}
	}()

	return impl
}

type DirectoryIteratorImpl interface {
	// Emits a node to the iterator.
	// The node won't be emitted if it has already been emitted (identified by the Id()).
	// Returns true if the node was emitted, false otherwise.
	MaybeEmit(DirectoryEntry) (bool, error)
}

type DirectoryIterator interface {
	// Returns a channel that emits nodes in the filesystem.
	// The order of nodes is not guaranteed.
	// Next is guaranteed to return all nodes in the filesystem exactly once.
	Chan() <-chan DirectoryEntry
	// Returns an error if one occurred during iteration.
	Err() error
	// Closes the iterator and releases resources.
	Close() error
}

type directoryIteratorImpl struct {
	mtx          sync.Mutex
	c            chan DirectoryEntry
	err          error
	emittedNodes map[int64]struct{}
}

func (n *directoryIteratorImpl) Chan() <-chan DirectoryEntry {
	return n.c
}

func (n *directoryIteratorImpl) MaybeEmit(node DirectoryEntry) (bool, error) {
	if _, ok := n.emittedNodes[node.ID()]; ok {
		return false, nil
	}

	n.emittedNodes[node.ID()] = struct{}{}
	n.c <- node
	if n.err != nil {
		return false, n.err
	}

	return true, nil
}

// Err implements DirectoryIterator.
func (n *directoryIteratorImpl) Err() error {
	return n.err
}

// Close implements DirectoryIterator.
func (n *directoryIteratorImpl) Close() error {
	n.mtx.Lock()
	defer n.mtx.Unlock()

	if n.err != nil {
		return n.err
	}

	n.err = io.EOF
	return nil
}

var (
	_ DirectoryIteratorImpl = &directoryIteratorImpl{}
	_ DirectoryIterator     = &directoryIteratorImpl{}
)

func NewDirectoryIterator(f func(DirectoryIteratorImpl) error) DirectoryIterator {
	impl := &directoryIteratorImpl{
		c:            make(chan DirectoryEntry),
		emittedNodes: make(map[int64]struct{}),
	}

	go func() {
		defer close(impl.c)

		if err := f(impl); err != nil {
			impl.err = err
		}
	}()

	return impl
}

type FilesystemReader interface {
	// Iterates through all nodes in the filesystem.
	IterateNodes() NodeIterator
	// Returns the root node of the filesystem.
	RootNode() (Node, error)
	// Returns a `io.Reader`+`io.ReaderAt` for a given inode.
	OpenContents(node Node) (ReaderHandle, error)
	// Returns a list of file entries in a directory.
	IterateDirectory(node Node) (DirectoryIterator, error)
}

type FilesystemWriter interface {
	// Allocate a new Inode on the filesystem. The node has metadata created at this time including modified time, permissions, ownership, etc...
	AllocateNode(node Node) error
	// Returns the root node of the filesystem.
	RootNode() (Node, error)
	// Set the contents of a file to a memory region.
	WriteContents(node Node, region vm.MemoryRegion) error
	// Write a list of pointers to a existing node turning that node into a directory.
	WriteDirectory(node Node, entries []DirectoryEntry) error
	// Writes final filesystem metadata and closes the filesystem.
	Finalize() error
}
