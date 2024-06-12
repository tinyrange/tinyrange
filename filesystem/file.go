package filesystem

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
)

type FileHandle interface {
	io.ReadCloser
}

type FileInfo interface {
	fs.FileInfo
}

type File interface {
	Open() (FileHandle, error)
	Stat() (FileInfo, error)
}

type FileType byte

const (
	TypeRegular FileType = iota
	TypeDirectory
	TypeSymlink
)

type Entry interface {
	File

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

const CACHE_ENTRY_SIZE = 1024

type CacheEntry struct {
	underlyingFile io.ReaderAt

	COffset   int64    `json:"o"`
	CTypeflag FileType `json:"t"`
	CName     string   `json:"n"`
	CLinkname string   `json:"l"`
	CSize     int64    `json:"s"`
	CMode     int64    `json:"m"`
	CUid      int      `json:"u"`
	CGid      int      `json:"g"`
	CModTime  int64    `json:"e"`
	CDevmajor int64    `json:"a"`
	CDevminor int64    `json:"i"`
}

// IsDir implements FileInfo.
func (e *CacheEntry) IsDir() bool {
	return e.Mode().IsDir()
}

// Sys implements FileInfo.
func (e *CacheEntry) Sys() any {
	return nil
}

// Open implements Entry.
func (e *CacheEntry) Open() (FileHandle, error) {
	return io.NopCloser(io.NewSectionReader(e.underlyingFile, e.COffset, e.CSize)), nil
}

// Stat implements Entry.
func (e *CacheEntry) Stat() (FileInfo, error) {
	return e, nil
}

func (e *CacheEntry) Typeflag() FileType { return e.CTypeflag }
func (e *CacheEntry) Name() string       { return e.CName }
func (e *CacheEntry) Linkname() string   { return e.CLinkname }
func (e *CacheEntry) Size() int64        { return e.CSize }
func (e *CacheEntry) Mode() fs.FileMode  { return fs.FileMode(e.CMode) }
func (e *CacheEntry) Uid() int           { return e.CUid }
func (e *CacheEntry) Gid() int           { return e.CGid }
func (e *CacheEntry) ModTime() time.Time { return time.UnixMicro(e.CModTime) }
func (e *CacheEntry) Devmajor() int64    { return e.CDevmajor }
func (e *CacheEntry) Devminor() int64    { return e.CDevminor }

var (
	_ Entry = &CacheEntry{}
)

type Archive interface {
	Entries() ([]Entry, error)
}

type ArrayArchive []Entry

// Entries implements Archive.
func (a ArrayArchive) Entries() ([]Entry, error) {
	return a, nil
}

var (
	_ Archive = ArrayArchive{}
)

type LocalFile struct {
	Filename string
}

// Open implements File.
func (l *LocalFile) Open() (FileHandle, error) {
	return os.Open(l.Filename)
}

// Stat implements File.
func (l *LocalFile) Stat() (FileInfo, error) {
	return os.Stat(l.Filename)
}

var (
	_ File = &LocalFile{}
)

func ReadArchiveFromFile(f File) (Archive, error) {
	fh, err := f.Open()
	if err != nil {
		return nil, err
	}

	readAt, ok := fh.(io.ReaderAt)
	if !ok {
		return nil, fmt.Errorf("%T does not support io.ReaderAt", fh)
	}

	var ret ArrayArchive

	var off int64 = 0

	hdrBytes := make([]byte, 1024)

	for {
		_, err := readAt.ReadAt(hdrBytes, off)
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		off += 1024

		hdrEnd := strings.IndexByte(string(hdrBytes), '\x00')

		var hdr CacheEntry

		if err := json.Unmarshal(hdrBytes[:hdrEnd], &hdr); err != nil {
			return nil, err
		}

		hdr.underlyingFile = readAt

		ret = append(ret, &hdr)

		off += hdr.CSize
	}

	return ret, nil
}
