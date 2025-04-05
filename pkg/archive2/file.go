package archive2

import (
	"io"
	"io/fs"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

type archiveFileHandle struct {
	*io.SectionReader
}

// Close implements filesystem.FileHandle.
func (a *archiveFileHandle) Close() error {
	return nil
}

var (
	_ filesystem.FileHandle = &archiveFileHandle{}
)

type archiveFile struct {
	contentsReader io.ReaderAt
	offset         int64

	modTime  time.Time
	mode     fs.FileMode
	size     int64
	name     string
	linkname string
	uid      int
	gid      int
}

// UidAndGid implements filesystem.HasUidAndGid.
func (a *archiveFile) UidAndGid() (int, int, error) {
	return a.uid, a.gid, nil
}

// LinkName implements filesystem.HasLinkName.
func (a *archiveFile) LinkName() (string, error) {
	return a.linkname, nil
}

// implements filesystem.FileInfo.
func (a *archiveFile) IsDir() bool               { return a.Mode().IsDir() }
func (a *archiveFile) Kind() filesystem.FileType { return filesystem.TypeRegular }
func (a *archiveFile) ModTime() time.Time        { return a.modTime }
func (a *archiveFile) Mode() fs.FileMode         { return a.mode }
func (a *archiveFile) Name() string              { return a.name }
func (a *archiveFile) Size() int64               { return a.size }
func (a *archiveFile) Sys() any                  { return nil }

// Open implements filesystem.File.
func (a *archiveFile) Open() (filesystem.FileHandle, error) {
	handle := io.NewSectionReader(a.contentsReader, a.offset, a.size)
	return &archiveFileHandle{
		SectionReader: handle,
	}, nil
}

// Stat implements filesystem.File.
func (a *archiveFile) Stat() (filesystem.FileInfo, error) {
	return a, nil
}

var (
	_ filesystem.File         = &archiveFile{}
	_ filesystem.HasLinkName  = &archiveFile{}
	_ filesystem.HasUidAndGid = &archiveFile{}
	_ filesystem.FileInfo     = &archiveFile{}
)

func (ar *ArchiveReader) File() (filesystem.File, error) {
	if ar.Kind() == EntryKindExtended {
		return fileFromExtendedEntry(ar)
	}

	if ar.Kind() != EntryKindRegular {
		return nil, fs.ErrInvalid
	}

	offset, err := ar.offset()
	if err != nil {
		return nil, err
	}

	uid, gid := ar.Owner()

	return &archiveFile{
		contentsReader: ar.contentsReader,
		offset:         offset,
		modTime:        ar.ModTime(),
		mode:           ar.Mode(),
		size:           ar.Size(),
		name:           ar.Name(),
		linkname:       ar.Linkname(),
		uid:            uid,
		gid:            gid,
	}, nil
}
