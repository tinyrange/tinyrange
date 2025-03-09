package filesystem

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem/vm"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

func GetLinkName(ent File) (string, error) {
	switch ent := ent.(type) {
	case HasLinkName:
		return ent.LinkName()
	case *memoryFile:
		if ent.kind != TypeSymlink && ent.kind != TypeLink {
			return "", fs.ErrInvalid
		}
		return string(ent.contents), nil
	case *overlayFile:
		return GetLinkName(ent.File)
	case simpleEntry:
		return ent.linkName, nil
	default:
		return "", fmt.Errorf("GetLinkName not implemented: %T", ent)
	}
}

func GetUidAndGid(ent File) (int, int, error) {
	switch ent := ent.(type) {
	case HasUidAndGid:
		return ent.UidAndGid()
	case *memoryDirectory:
		return GetUidAndGid(ent.memoryFile)
	case *memoryFile:
		return ent.uid, ent.gid, nil
	case *overlayFile:
		return ent.uid, ent.gid, nil
	case simpleEntry:
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

type remoteFile struct {
	client   *http.Client
	url      string
	contents MutableFile
}

func (r *remoteFile) loadContents() error {
	resp, err := r.client.Get(r.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	contents, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	r.contents = NewMemoryFile(TypeRegular)

	if err := r.contents.Overwrite(contents); err != nil {
		return err
	}

	return nil
}

// Open implements File.
func (r *remoteFile) Open() (FileHandle, error) {
	if r.contents == nil {
		if err := r.loadContents(); err != nil {
			return nil, err
		}
	}

	return r.contents.Open()
}

// Stat implements File.
func (r *remoteFile) Stat() (FileInfo, error) {
	if r.contents == nil {
		if err := r.loadContents(); err != nil {
			return nil, err
		}
	}

	return r.contents.Stat()
}

var (
	_ File = &remoteFile{}
)

func NewRemoteFile(client *http.Client, url string) File {
	return &remoteFile{client: client, url: url}
}

type lazyRemoteFile struct {
	client       *http.Client
	url          string
	expectedSize int64
	region       vm.RawRegion
}

// ReadAt implements io.ReaderAt.
func (l *lazyRemoteFile) ReadAt(p []byte, off int64) (n int, err error) {
	if l.region == nil {
		resp, err := l.client.Get(l.url)
		if err != nil {
			slog.Error("failed to read", "err", err)
			return -1, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return -1, fmt.Errorf("bad status: %s", resp.Status)
		}

		l.region = make(vm.RawRegion, l.expectedSize)

		if _, err = io.ReadFull(resp.Body, l.region); err != nil {
			slog.Error("failed to read", "err", err)
			return -1, err
		}
	}

	return l.region.ReadAt(p, off)
}

var (
	_ io.ReaderAt = &lazyRemoteFile{}
)

func NewLazyRemoteFile(client *http.Client, url string, expectedSize int64) io.ReaderAt {
	return &lazyRemoteFile{client: client, url: url, expectedSize: expectedSize}
}

type overlayFile struct {
	File

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

type memoryFile struct {
	mtx      sync.RWMutex
	kind     FileType
	mTime    time.Time
	mode     fs.FileMode
	uid      int
	gid      int
	contents []byte
}

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

type simpleEntry struct {
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

func (s simpleEntry) LinkName() (string, error)    { return s.linkName, nil }
func (s simpleEntry) UidAndGid() (int, int, error) { return s.uid, s.gid, nil }
func (s simpleEntry) Devmajor() int64              { return 0 }
func (s simpleEntry) Devminor() int64              { return 0 }
func (s simpleEntry) Uid() int                     { return s.uid }
func (s simpleEntry) Gid() int                     { return s.gid }
func (s simpleEntry) Linkname() string             { return s.linkName }
func (s simpleEntry) ModTime() time.Time           { return s.modTime }
func (s simpleEntry) Mode() fs.FileMode            { return s.mode }
func (s simpleEntry) Name() string                 { return s.name }
func (s simpleEntry) Size() int64                  { return s.size }
func (s simpleEntry) Typeflag() FileType           { return s.typeFlag }

var (
	_ Entry = simpleEntry{}
)
