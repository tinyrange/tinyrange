package filesystem

import (
	"io"
	"io/fs"
	"net/http"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
)

// BasicFileHandle is a basic file handle that can be used for reading but doesn't support being closed.
type BasicFileHandle interface {
	io.Reader
	io.ReaderAt
}

// FileHandle is a file handle that can be used for reading and supports being closed.
type FileHandle interface {
	BasicFileHandle
	io.Closer
}

// WritableFileHandle is a file handle that can be used for writing and supports being closed.
type WritableFileHandle interface {
	FileHandle
	io.Writer
	io.WriterAt
}

// FileInfo is a file info interface that extends the standard fs.FileInfo interface.
type FileInfo interface {
	fs.FileInfo

	Kind() FileType
}

type ExtendedFileMethods interface {
	// HttpClient returns an http client.
	HttpClient() (*http.Client, error)

	// GetOrSetCacheForHash returns a cached file for the given hash.
	GetOrSetCacheForHash(hash string, setter func(w io.Writer) error) (io.ReaderAt, error)
}

type HasOpenRegion interface {
	OpenRegion() (vm.MemoryRegion, error)
}

// File is an interface that represents a file.
type File interface {
	Open() (FileHandle, error)
	Stat() (FileInfo, error)
}

type Symlink interface {
	Readlink() (string, error)
	Lstat() (FileInfo, error)
}

type HostFile interface {
	File

	Filename() (string, error)
}

// MutableFile is a file that can be modified.
type MutableFile interface {
	File

	// OpenMut opens the file for writing.
	OpenMut() (WritableFileHandle, error)

	// Chmod changes the mode of the file.
	Chmod(mode fs.FileMode) error

	// Chown changes the owner and group of the file.
	// If uid or gid is -1, the corresponding value is not changed.
	Chown(uid int, gid int) error

	// Chtimes changes the modification time of the file.
	Chtimes(mtime time.Time) error

	// Overwrite overwrites the contents of the file.
	Overwrite(contents []byte) error

	// Truncate truncates the file to the specified size.
	Truncate(size int64) error
}

type HasLinkName interface {
	File
	LinkName() (string, error)
}

type HasUidAndGid interface {
	File
	UidAndGid() (int, int, error)
}

// Entry is a single file in an archive.
type Entry interface {
	File
	HasLinkName
	HasUidAndGid

	Typeflag() FileType

	Name() string     // Name of file entry
	Linkname() string // Target name of link (valid for TypeLink or TypeSymlink)

	Size() int64       // Logical file size in bytes
	Mode() fs.FileMode // Permission and mode bits
	Uid() int          // User ID of owner
	Gid() int          // Group ID of owner

	ModTime() time.Time // Modification time

	Devmajor() int64 // Major device number (valid for TypeChar or TypeBlock)
	Devminor() int64 // Minor device number (valid for TypeChar or TypeBlock)
}

// DirectoryEntry is a single file in a directory.
type DirectoryEntry struct {
	File
	// Name of file entry
	Name string
}

// Directory is an interface that represents a directory.
type Directory interface {
	File

	// GetChild gets a child of the directory.
	GetChild(name string) (DirectoryEntry, error)
	// Readdir reads the directory and returns a list of entries.
	Readdir() ([]DirectoryEntry, error)
}

// MutableDirectory is a directory that can be modified.
type MutableDirectory interface {
	Directory
	MutableFile

	// Mkdir creates a new directory.
	// If the directory already exists it is returned.
	Mkdir(name string) (MutableDirectory, error)
	// Create creates a new file and optionally overwrites its contents with f.
	Create(name string, f File) (File, error)
	// Unlink removes a file or directory.
	Unlink(name string) error
}

type MutableRenameFile interface {
	MutableFile

	// Rename renames a file or directory.
	Rename(newDirectory MutableDirectory, newName string) error
}

// Archive is an interface that represents a list of entries.
type Archive interface {
	// Entries returns a list of entries in the archive.
	Entries() ([]Entry, error)
}

type StreamableTempFile interface {
	io.WriteCloser
	FilenameAndHash() (string, string)
}

type StreamableWriter interface {
	Writer() (StreamableTempFile, error)
}
