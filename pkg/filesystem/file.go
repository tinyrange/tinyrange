package filesystem

import (
	"fmt"
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/tinyrange/tinyrange/pkg/hash"
)

func GetLinkName(ent File) (string, error) {
	switch ent := ent.(type) {
	case HasLinkName:
		return ent.LinkName()
	case DirectoryEntry:
		return GetLinkName(ent.File)
	case *memoryFile:
		if ent.kind != TypeSymlink && ent.kind != TypeLink {
			return "", fs.ErrInvalid
		}
		return string(ent.contents), nil
	case *overlayFile:
		return GetLinkName(ent.File)
	default:
		return "", fmt.Errorf("GetLinkName not implemented: %T", ent)
	}
}

func GetUidAndGid(ent File) (int, int, error) {
	switch ent := ent.(type) {
	case HasUidAndGid:
		return ent.UidAndGid()
	case DirectoryEntry:
		return GetUidAndGid(ent.File)
	case *memoryDirectory:
		return GetUidAndGid(ent.memoryFile)
	case *memoryFile:
		return ent.uid, ent.gid, nil
	case *overlayFile:
		return ent.uid, ent.gid, nil
	case *localFile:
		return 0, 0, nil
	case *localDirectory:
		return 0, 0, nil
	case *localMutableFile:
		stat, err := ent.Stat()
		if err != nil {
			return -1, -1, err
		}

		return GetUidAndGidNative(stat)
	case *localMutableDirectory:
		stat, err := ent.Stat()
		if err != nil {
			return -1, -1, err
		}

		return GetUidAndGidNative(stat)
	default:
		return -1, -1, fmt.Errorf("GetUidAndGid not implemented: %T", ent)
	}
}

type FileType byte

const (
	TypeRegular FileType = iota
	TypeDirectory
	TypeSymlink
	TypeLink
	TypeDeleted
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
	case TypeDeleted:
		return "Deleted"
	default:
		return "<unknown>"
	}
}

const OVERLAY_FILE_ID = 0xcafecafe_00000000

type overlayFile struct {
	File

	id    uint32
	kind  FileType
	size  int64
	mTime time.Time
	mode  fs.FileMode
	uid   int
	gid   int
}

// OpenMut implements MutableFile.
func (m *overlayFile) OpenMut() (WritableFileHandle, error) {
	return nil, fmt.Errorf("OverlayFiles do not support being opened for writing")
}

func (m *overlayFile) Id() uint64         { return hashId(OVERLAY_FILE_ID + uint64(m.id)) }
func (m *overlayFile) Kind() FileType     { return m.kind }
func (m *overlayFile) IsDir() bool        { return false }
func (m *overlayFile) ModTime() time.Time { return m.mTime }
func (m *overlayFile) Mode() fs.FileMode  { return m.mode }
func (m *overlayFile) Name() string       { return "" }
func (m *overlayFile) Size() int64        { return m.size }
func (m *overlayFile) Sys() any           { return m }

// Chmod implements MutableFile.
func (m *overlayFile) Chmod(mode fs.FileMode) error {
	m.mode = mode

	return nil
}

// Chown implements MutableFile.
func (m *overlayFile) Chown(uid int, gid int) error {
	if uid > -1 {
		m.uid = uid
	}
	if gid > -1 {
		m.gid = gid
	}

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

// Truncate implements MutableFile.
func (o *overlayFile) Truncate(size int64) error {
	return fmt.Errorf("OverlayFiles do not support being truncated")
}

// Stat implements MutableFile.
// Subtle: this method shadows the method (File).Stat of OverlayFile.File.
func (o *overlayFile) Stat() (FileInfo, error) {
	return o, nil
}

var (
	_ MutableFile = &overlayFile{}
)

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

type sourceWrapper struct {
	File
	source hash.SerializableValue
}

func NewSourceWrapper(f File, source hash.SerializableValue) File {
	return &sourceWrapper{File: f, source: source}
}

type HasSource interface {
	Source() (hash.SerializableValue, error)
}

func SourceFromFile(f File) (hash.SerializableValue, error) {
	switch f := f.(type) {
	case HasSource:
		return f.Source()
	case *localFile:
		if f.source == nil {
			return nil, fmt.Errorf("localFile at %s has no source", f.filename)
		}
		return f.source, nil
	case *localMutableFile:
		if f.source == nil {
			return nil, fmt.Errorf("localMutableFile at %s has no source", f.filename)
		}
		return f.source, nil
	case *sourceWrapper:
		return f.source, nil
	case DirectoryEntry:
		return SourceFromFile(f.File)
	default:
		return nil, fmt.Errorf("SourceFromFile not implemented: %T %+v", f, f)
	}
}

func SourceFromArchive(a Archive) (hash.SerializableValue, error) {
	switch a := a.(type) {
	case HasSource:
		return a.Source()
	default:
		return nil, fmt.Errorf("SourceFromArchive not implemented: %T %+v", a, a)
	}
}

type memoryFileHandle struct {
	f        *memoryFile
	offMutex sync.Mutex
	offset   int64
}

// Read implements io.Reader.
func (m *memoryFileHandle) Read(p []byte) (n int, err error) {
	n, err = m.ReadAt(p, m.offset)

	m.offMutex.Lock()
	defer m.offMutex.Unlock()

	m.offset += int64(n)

	return
}

// ReadAt implements io.ReaderAt.
func (m *memoryFileHandle) ReadAt(p []byte, off int64) (n int, err error) {
	m.f.mtx.RLock()
	defer m.f.mtx.RUnlock()

	if off < 0 || off >= int64(len(m.f.contents)) {
		return 0, io.EOF
	}

	n = copy(p, m.f.contents[off:])
	if n < len(p) {
		err = io.EOF
	}

	return
}

// Close implements io.Closer.
func (m *memoryFileHandle) Close() error {
	return nil
}

// Write implements io.Writer.
func (m *memoryFileHandle) Write(p []byte) (n int, err error) {
	n, err = m.WriteAt(p, m.offset)

	m.offMutex.Lock()
	defer m.offMutex.Unlock()

	m.offset += int64(n)

	return
}

// WriteAt implements io.WriterAt.
func (m *memoryFileHandle) WriteAt(p []byte, off int64) (n int, err error) {
	m.f.mtx.Lock()
	defer m.f.mtx.Unlock()

	if off < 0 {
		return 0, fmt.Errorf("negative offset")
	}

	if off+int64(len(p)) > int64(len(m.f.contents)) {
		// Extend the file.
		m.f.contents = append(m.f.contents, make([]byte, int(off+int64(len(p)))-len(m.f.contents))...)
	}

	n = copy(m.f.contents[off:], p)

	return
}

var (
	_ WritableFileHandle = &memoryFileHandle{}
)

const MEMORy_FILE_ID = 0xdeadbeef_00000000

type memoryFile struct {
	id       uint32
	fac      FileFactory
	mtx      sync.RWMutex
	kind     FileType
	mTime    time.Time
	mode     fs.FileMode
	uid      int
	gid      int
	contents []byte
}

func (m *memoryFile) Id() uint64         { return hashId(MEMORy_FILE_ID + uint64(m.id)) }
func (m *memoryFile) Kind() FileType     { return m.kind }
func (m *memoryFile) IsDir() bool        { return false }
func (m *memoryFile) ModTime() time.Time { return m.mTime }
func (m *memoryFile) Mode() fs.FileMode  { return m.mode }
func (m *memoryFile) Name() string       { return "" }
func (m *memoryFile) Size() int64        { return int64(len(m.contents)) }
func (m *memoryFile) Sys() any           { return m }

// Chmod implements MutableFile.
func (m *memoryFile) Chmod(mode fs.FileMode) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.mode = mode

	return nil
}

// Chown implements MutableFile.
func (m *memoryFile) Chown(uid int, gid int) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if uid > -1 {
		m.uid = uid
	}
	if gid > -1 {
		m.gid = gid
	}

	return nil
}

// Chtimes implements MutableFile.
func (m *memoryFile) Chtimes(mtime time.Time) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.mTime = mtime

	return nil
}

// Open implements MutableFile.
func (m *memoryFile) Open() (FileHandle, error) {
	return m.OpenMut()
}

// OpenMut implements MutableFile.
func (m *memoryFile) OpenMut() (WritableFileHandle, error) {
	return &memoryFileHandle{f: m}, nil
}

// Overwrite implements MutableFile.
func (m *memoryFile) Overwrite(contents []byte) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.contents = contents

	return nil
}

// Truncate implements MutableFile.
func (m *memoryFile) Truncate(size int64) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if size < 0 {
		return fmt.Errorf("negative size")
	}

	if size > int64(len(m.contents)) {
		m.contents = append(m.contents, make([]byte, size-int64(len(m.contents)))...)
	} else {
		m.contents = m.contents[:size]
	}

	return nil
}

// Stat implements MutableFile.
func (m *memoryFile) Stat() (FileInfo, error) {
	return m, nil
}

var (
	_ MutableFile = &memoryFile{}
)

type simpleFileHandle struct {
	io.SectionReader
}

// Close implements FileHandle.
func (s *simpleFileHandle) Close() error {
	return nil
}

func NewSimpleFileHandle(r io.ReaderAt, size int64) FileHandle {
	return &simpleFileHandle{
		SectionReader: *io.NewSectionReader(r, 0, size),
	}
}
