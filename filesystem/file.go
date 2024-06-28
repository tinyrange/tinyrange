package filesystem

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

func GetLinkName(ent File) (string, error) {
	switch ent := ent.(type) {
	case *CacheEntry:
		return ent.CLinkname, nil
	default:
		return "", fmt.Errorf("GetLinkName not implemented: %T", ent)
	}
}

func GetUidAndGid(ent File) (int, int, error) {
	switch ent := ent.(type) {
	case *StarDirectory:
		return GetUidAndGid(ent.Directory)
	case *StarFile:
		return GetUidAndGid(ent.File)
	case *memoryDirectory:
		return GetUidAndGid(ent.memoryFile)
	case *memoryFile:
		return ent.uid, ent.gid, nil
	case *CacheEntry:
		return ent.CUid, ent.CGid, nil
	default:
		return -1, -1, fmt.Errorf("GetUidAndGid not implemented: %T", ent)
	}
}

type BasicFileHandle interface {
	io.Reader
	io.ReaderAt
}

type FileHandle interface {
	BasicFileHandle
	io.Closer
}

type nopCloserFileHandle struct {
	BasicFileHandle
}

// Close implements FileHandle.
func (n *nopCloserFileHandle) Close() error { return nil }

var (
	_ FileHandle = &nopCloserFileHandle{}
)

func NewNopCloserFileHandle(fh BasicFileHandle) FileHandle {
	return &nopCloserFileHandle{BasicFileHandle: fh}
}

type FileInfo interface {
	fs.FileInfo
}

type FileDigest struct {
	Hash string
}

type File interface {
	Open() (FileHandle, error)
	Stat() (FileInfo, error)

	// Returns nil if it's not supported.
	Digest() *FileDigest
}

type MutableFile interface {
	File

	Chmod(mode fs.FileMode) error
	Chown(uid int, gid int) error
	Chtimes(mtime time.Time) error
	Overwrite(contents []byte) error
}

type FileType byte

const (
	TypeRegular FileType = iota
	TypeDirectory
	TypeSymlink
	TypeLink
)

func (t FileType) String() string {
	switch t {
	case TypeRegular:
		return "Regular"
	case TypeDirectory:
		return "Directory"
	case TypeSymlink:
		return "Symlink"
	case TypeLink:
		return "Link"
	default:
		return "<unknown>"
	}
}

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

// Digest implements Entry.
func (e *CacheEntry) Digest() *FileDigest {
	return nil
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
	return NewNopCloserFileHandle(io.NewSectionReader(e.underlyingFile, e.COffset, e.CSize)), nil
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

type LocalFile struct {
	Filename string
}

// Digest implements File.
func (l *LocalFile) Digest() *FileDigest {
	return &FileDigest{Hash: l.Filename}
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

func NewLocalFile(filename string) File {
	return &LocalFile{Filename: filename}
}

type memoryFile struct {
	mTime    time.Time
	mode     fs.FileMode
	uid      int
	gid      int
	contents []byte
}

// Digest implements MutableFile.
func (m *memoryFile) Digest() *FileDigest {
	return nil
}

// IsDir implements FileInfo.
func (m *memoryFile) IsDir() bool {
	return false
}

// ModTime implements FileInfo.
func (m *memoryFile) ModTime() time.Time {
	return m.mTime
}

// Mode implements FileInfo.
func (m *memoryFile) Mode() fs.FileMode {
	return m.mode
}

// Name implements FileInfo.
func (m *memoryFile) Name() string {
	return ""
}

// Size implements FileInfo.
func (m *memoryFile) Size() int64 {
	return int64(len(m.contents))
}

// Sys implements FileInfo.
func (m *memoryFile) Sys() any {
	return m
}

// Chmod implements MutableFile.
func (m *memoryFile) Chmod(mode fs.FileMode) error {
	m.mode = mode

	return nil
}

// Chown implements MutableFile.
func (m *memoryFile) Chown(uid int, gid int) error {
	m.uid = uid
	m.gid = gid

	return nil
}

// Chtimes implements MutableFile.
func (m *memoryFile) Chtimes(mtime time.Time) error {
	m.mTime = mtime

	return nil
}

// Open implements MutableFile.
func (m *memoryFile) Open() (FileHandle, error) {
	return NewNopCloserFileHandle(bytes.NewReader(m.contents)), nil
}

// Overwrite implements MutableFile.
func (m *memoryFile) Overwrite(contents []byte) error {
	m.contents = contents

	return nil
}

// Stat implements MutableFile.
func (m *memoryFile) Stat() (FileInfo, error) {
	return m, nil
}

var (
	_ MutableFile = &memoryFile{}
)

func NewMemoryFile() MutableFile {
	return &memoryFile{
		mode:  fs.FileMode(0755),
		mTime: time.Now(),
	}
}
