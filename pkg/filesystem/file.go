package filesystem

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/hash"
)

func GetLinkName(ent File) (string, error) {
	switch ent := ent.(type) {
	case *StarFile:
		return GetLinkName(ent.File)
	case *CacheEntry:
		return ent.CLinkname, nil
	case *memoryFile:
		if ent.kind != TypeSymlink && ent.kind != TypeLink {
			return "", fs.ErrInvalid
		}
		return string(ent.contents), nil
	case SimpleEntry:
		return ent.linkName, nil
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
	case SimpleEntry:
		return ent.uid, ent.gid, nil
	case *LocalFile:
		return 0, 0, nil // local files are normally build definitions.
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

type WritableFileHandle interface {
	FileHandle
	io.Writer
	io.WriterAt
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

	Kind() FileType
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

type osStat struct {
	fs.FileInfo
}

// Kind implements FileInfo.
func (o *osStat) Kind() FileType {
	if o.IsDir() {
		return TypeDirectory
	} else if o.Mode().Type() == fs.ModeSymlink {
		return TypeSymlink
	} else {
		return TypeRegular
	}
}

var (
	_ FileInfo = &osStat{}
)

type LocalFile struct {
	filename string
	source   hash.SerializableValue
}

// Digest implements File.
func (l *LocalFile) Digest() *FileDigest {
	return &FileDigest{Hash: l.filename}
}

// Open implements File.
func (l *LocalFile) Open() (FileHandle, error) {
	return os.Open(l.filename)
}

// Stat implements File.
func (l *LocalFile) Stat() (FileInfo, error) {
	s, err := os.Stat(l.filename)
	if err != nil {
		return nil, err
	}

	return &osStat{FileInfo: s}, nil
}

var (
	_ File = &LocalFile{}
)

func NewLocalFile(filename string, source hash.SerializableValue) File {
	return &LocalFile{filename: filename, source: source}
}

type overlayFile struct {
	File

	kind     FileType
	size     int64
	mTime    time.Time
	mode     fs.FileMode
	uid      int
	gid      int
	contents []byte
}

func (m *overlayFile) Kind() FileType      { return m.kind }
func (m *overlayFile) Digest() *FileDigest { return nil }
func (m *overlayFile) IsDir() bool         { return false }
func (m *overlayFile) ModTime() time.Time  { return m.mTime }
func (m *overlayFile) Mode() fs.FileMode   { return m.mode }
func (m *overlayFile) Name() string        { return "" }
func (m *overlayFile) Size() int64         { return int64(len(m.contents)) }
func (m *overlayFile) Sys() any            { return m }

// Chmod implements MutableFile.
func (m *overlayFile) Chmod(mode fs.FileMode) error {
	m.mode = mode

	return nil
}

// Chown implements MutableFile.
func (m *overlayFile) Chown(uid int, gid int) error {
	m.uid = uid
	m.gid = gid

	return nil
}

// Chtimes implements MutableFile.
func (m *overlayFile) Chtimes(mtime time.Time) error {
	m.mTime = mtime

	return nil
}

// Overwrite implements MutableFile.
func (o *overlayFile) Overwrite(contents []byte) error {
	return fmt.Errorf("OverlayFiles do not support being overwritten")
}

// Stat implements MutableFile.
// Subtle: this method shadows the method (File).Stat of OverlayFile.File.
func (o *overlayFile) Stat() (FileInfo, error) {
	return o, nil
}

var (
	_ MutableFile = &overlayFile{}
)

func NewOverlayFile(underlying File) (MutableFile, error) {
	info, err := underlying.Stat()
	if err != nil {
		return nil, err
	}

	return &overlayFile{
		File:  underlying,
		mode:  fs.FileMode(0755),
		mTime: time.Now(),
		kind:  info.Kind(),
		size:  info.Size(),
	}, nil
}

type ChildSource struct {
	Source hash.SerializableValue
	Name   string
}

// SerializableType implements hash.SerializableValue.
func (c ChildSource) SerializableType() string {
	return "ChildSource"
}

var (
	_ hash.SerializableValue = ChildSource{}
)

func SourceFromFile(f File) (hash.SerializableValue, error) {
	switch f := f.(type) {
	case *LocalFile:
		return f.source, nil
	case *StarFile:
		return SourceFromFile(f.File)
	case *CacheEntry:
		if f.underlyingSource != nil {
			return ChildSource{
				Source: f.underlyingSource,
				Name:   f.CName,
			}, nil
		} else {
			return nil, fmt.Errorf("CacheEntry has no source")
		}
	default:
		return nil, fmt.Errorf("SourceFromFile not implemented: %T %+v", f, f)
	}
}

type memoryFile struct {
	kind     FileType
	mTime    time.Time
	mode     fs.FileMode
	uid      int
	gid      int
	contents []byte
}

func (m *memoryFile) Kind() FileType      { return m.kind }
func (m *memoryFile) Digest() *FileDigest { return nil }
func (m *memoryFile) IsDir() bool         { return false }
func (m *memoryFile) ModTime() time.Time  { return m.mTime }
func (m *memoryFile) Mode() fs.FileMode   { return m.mode }
func (m *memoryFile) Name() string        { return "" }
func (m *memoryFile) Size() int64         { return int64(len(m.contents)) }
func (m *memoryFile) Sys() any            { return m }

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

func NewMemoryFile(kind FileType) MutableFile {
	return &memoryFile{
		kind:  kind,
		mode:  fs.FileMode(0755),
		mTime: time.Now(),
	}
}

func NewSymlink(target string) MutableFile {
	return &memoryFile{
		kind:     TypeSymlink,
		mode:     fs.FileMode(0755),
		contents: []byte(target),
	}
}

func NewHardLink(target string) (MutableFile, error) {
	target = strings.TrimPrefix(target, ".")
	if !strings.HasPrefix(target, "/") {
		target = "/" + target
	}

	return &memoryFile{
		kind:     TypeLink,
		mode:     fs.FileMode(0755),
		contents: []byte(target),
	}, nil
}

type SimpleEntry struct {
	File

	uid      int
	gid      int
	linkName string
	modTime  time.Time
	mode     fs.FileMode
	name     string
	size     int64
	typeFlag FileType
}

func (s SimpleEntry) Devmajor() int64    { return 0 }
func (s SimpleEntry) Devminor() int64    { return 0 }
func (s SimpleEntry) Uid() int           { return s.uid }
func (s SimpleEntry) Gid() int           { return s.gid }
func (s SimpleEntry) Linkname() string   { return s.linkName }
func (s SimpleEntry) ModTime() time.Time { return s.modTime }
func (s SimpleEntry) Mode() fs.FileMode  { return s.mode }
func (s SimpleEntry) Name() string       { return s.name }
func (s SimpleEntry) Size() int64        { return s.size }
func (s SimpleEntry) Typeflag() FileType { return s.typeFlag }

var (
	_ Entry = SimpleEntry{}
)
