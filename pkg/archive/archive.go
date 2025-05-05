package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/fsutil"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

type arrayArchive []filesystem.Entry

// Entries implements Archive.
func (a arrayArchive) Entries() ([]filesystem.Entry, error) {
	return a, nil
}

var (
	_ filesystem.Archive = arrayArchive{}
)

func ReadArchiveFromFile(f filesystem.File) (filesystem.Archive, error) {
	fh, err := f.Open()
	if err != nil {
		return nil, err
	}

	var source hash.SerializableValue

	if src, err := filesystem.SourceFromFile(f); err == nil {
		source = src
	}

	var ret arrayArchive

	var off int64 = 0

	hdrBytes := make([]byte, 1024)

	for {
		_, err := fh.ReadAt(hdrBytes, off)
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		off += 1024

		hdrEnd := strings.IndexByte(string(hdrBytes), '\x00')
		if hdrEnd == -1 {
			return nil, fmt.Errorf("invalid header: %s", hdrBytes)
		}

		var hdr cacheEntry

		if err := json.Unmarshal(hdrBytes[:hdrEnd], &hdr); err != nil {
			return nil, err
		}

		hdr.underlyingFile = fh
		hdr.underlyingSource = source

		ret = append(ret, &hdr)

		off += hdr.CSize
	}

	return ret, nil
}

func extractEntry(ent filesystem.Entry, dir filesystem.MutableDirectory) (filesystem.File, error) {
	switch ent.Typeflag() {
	case filesystem.TypeDirectory:
		name := strings.TrimSuffix(ent.Name(), "/")
		name = strings.TrimPrefix(name, "./")

		child, err := fsutil.Mkdir(dir, name)
		if errors.Is(err, os.ErrExist) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}

		if err := child.Chmod(ent.Mode()); err != nil {
			return nil, err
		}

		if err := child.Chown(ent.Uid(), ent.Gid()); err != nil {
			return nil, err
		}

		if err := child.Chtimes(ent.ModTime()); err != nil {
			return nil, err
		}

		return child, nil
	case filesystem.TypeRegular:
		return fsutil.CreateChild(dir, ent.Name(), ent)
	case filesystem.TypeSymlink:
		return fsutil.CreateChild(dir, ent.Name(), ent)
	case filesystem.TypeLink:
		return fsutil.CreateChild(dir, ent.Name(), ent)
	default:
		return nil, fmt.Errorf("unknown Entry type: %s", ent.Typeflag())
	}
}

func ExtractArchive(ark filesystem.Archive, mut filesystem.MutableDirectory) error {
	ents, err := ark.Entries()
	if err != nil {
		return err
	}

	for _, ent := range ents {
		if _, err := extractEntry(ent, mut); err != nil {
			return fmt.Errorf("failed to extract archive: %w", err)
		}
	}

	// if err := ValidateAndDump(os.Stdout, mut); err != nil {
	// 	return err
	// }

	return nil
}

type nopCloserFileHandle struct {
	filesystem.BasicFileHandle
}

// Close implements FileHandle.
func (n *nopCloserFileHandle) Close() error { return nil }

var (
	_ filesystem.FileHandle = &nopCloserFileHandle{}
)

func newNopCloserFileHandle(fh filesystem.BasicFileHandle) filesystem.FileHandle {
	return &nopCloserFileHandle{BasicFileHandle: fh}
}

const CACHE_ENTRY_SIZE = 1024

type cacheEntry struct {
	underlyingFile   io.ReaderAt
	underlyingSource hash.SerializableValue

	COffset   int64               `json:"o"`
	CTypeflag filesystem.FileType `json:"t"`
	CName     string              `json:"n"`
	CLinkname string              `json:"l"`
	CSize     int64               `json:"s"`
	CMode     int64               `json:"m"`
	CUid      int                 `json:"u"`
	CGid      int                 `json:"g"`
	CModTime  int64               `json:"e"` // in microseconds since the unix epoch.
	CDevmajor int64               `json:"a"`
	CDevminor int64               `json:"i"`

	// Used for streaming files only.
	Hash             string `json:"hash,omitempty"`
	ContentsFilename string `json:"contents,omitempty"`
}

// Source implements filesystem.HasSource.
func (e *cacheEntry) Source() (hash.SerializableValue, error) {
	if e.underlyingSource != nil {
		return filesystem.ChildSource{
			Source: e.underlyingSource,
			Name:   e.CName,
		}, nil
	} else {
		return nil, fmt.Errorf("CacheEntry has no source")
	}
}

// LinkName implements filesystem.Entry.
func (e *cacheEntry) LinkName() (string, error) {
	return e.CLinkname, nil
}

// UidAndGid implements filesystem.Entry.
func (e *cacheEntry) UidAndGid() (int, int, error) {
	return e.CUid, e.CGid, nil
}

// IsDir implements FileInfo.
func (e *cacheEntry) IsDir() bool {
	return e.Mode().IsDir()
}

// Sys implements FileInfo.
func (e *cacheEntry) Sys() any {
	return nil
}

// Open implements Entry.
func (e *cacheEntry) Open() (filesystem.FileHandle, error) {
	if e.CTypeflag != filesystem.TypeRegular {
		return nil, fmt.Errorf("file is not a regular file: %s", e.CTypeflag.String())
	}
	return newNopCloserFileHandle(
		io.NewSectionReader(e.underlyingFile, e.COffset, e.CSize),
	), nil
}

// Stat implements Entry.
func (e *cacheEntry) Stat() (filesystem.FileInfo, error) {
	return e, nil
}

func (e *cacheEntry) Kind() filesystem.FileType     { return e.CTypeflag }
func (e *cacheEntry) Typeflag() filesystem.FileType { return e.CTypeflag }
func (e *cacheEntry) Name() string                  { return e.CName }
func (e *cacheEntry) Linkname() string              { return e.CLinkname }
func (e *cacheEntry) Size() int64                   { return e.CSize }
func (e *cacheEntry) Mode() fs.FileMode             { return fs.FileMode(e.CMode) }
func (e *cacheEntry) Uid() int                      { return e.CUid }
func (e *cacheEntry) Gid() int                      { return e.CGid }
func (e *cacheEntry) ModTime() time.Time            { return time.UnixMicro(e.CModTime) }
func (e *cacheEntry) Devmajor() int64               { return e.CDevmajor }
func (e *cacheEntry) Devminor() int64               { return e.CDevminor }

var (
	_ filesystem.Entry     = &cacheEntry{}
	_ filesystem.HasSource = &cacheEntry{}
)

type ArchiveWriter struct {
	w      io.Writer
	offset int64
}

func (w *ArchiveWriter) WriteEntry(e filesystem.Entry, r io.Reader) error {
	ent, ok := e.(*cacheEntry)
	if !ok {
		ent = NewEntryBuilder().CloneFrom(e).Build().(*cacheEntry)
	}

	ent.COffset = w.offset + 1024

	bytes, err := json.Marshal(&ent)
	if err != nil {
		return err
	}

	if len(bytes) > CACHE_ENTRY_SIZE {
		return fmt.Errorf("oversized entry header: %d > %d", len(bytes), CACHE_ENTRY_SIZE)
	} else if len(bytes) < CACHE_ENTRY_SIZE {
		tmp := make([]byte, CACHE_ENTRY_SIZE)
		copy(tmp, bytes)
		bytes = tmp
	}

	childN, err := w.w.Write(bytes)
	if err != nil {
		return err
	}

	w.offset += int64(childN)

	if r != nil {
		childN64, err := io.CopyN(w.w, r, ent.CSize)
		if err != nil {
			return err
		}

		w.offset += childN64
	}

	return nil
}

func NewArchiveWriter(w io.Writer) *ArchiveWriter {
	return &ArchiveWriter{w: w}
}

type EntryBuilder struct {
	entry *cacheEntry
}

func (b *EntryBuilder) Typeflag(t filesystem.FileType) *EntryBuilder {
	b.entry.CTypeflag = t
	return b
}

func (b *EntryBuilder) Name(n string) *EntryBuilder {
	b.entry.CName = n
	return b
}

func (b *EntryBuilder) Linkname(n string) *EntryBuilder {
	b.entry.CLinkname = n
	return b
}

func (b *EntryBuilder) Size(s int64) *EntryBuilder {
	b.entry.CSize = s
	return b
}

func (b *EntryBuilder) Mode(m fs.FileMode) *EntryBuilder {
	b.entry.CMode = int64(m)
	return b
}

func (b *EntryBuilder) UidAndGid(uid, gid int) *EntryBuilder {
	b.entry.CUid = uid
	b.entry.CGid = gid
	return b
}

func (b *EntryBuilder) ModTime(t time.Time) *EntryBuilder {
	b.entry.CModTime = t.UnixMicro()
	return b
}

func (b *EntryBuilder) Device(major, minor int64) *EntryBuilder {
	b.entry.CDevmajor = major
	b.entry.CDevminor = minor
	return b
}

func (b *EntryBuilder) CloneFrom(ent filesystem.Entry) *EntryBuilder {
	b.entry.CTypeflag = ent.Typeflag()
	b.entry.CName = ent.Name()
	b.entry.CLinkname = ent.Linkname()
	b.entry.CSize = ent.Size()
	b.entry.CMode = int64(ent.Mode())
	b.entry.CUid, b.entry.CGid = ent.Uid(), ent.Gid()
	b.entry.CModTime = ent.ModTime().UnixMicro()
	b.entry.CDevmajor, b.entry.CDevminor = ent.Devmajor(), ent.Devminor()
	return b
}

func (b *EntryBuilder) Build() filesystem.Entry {
	return b.entry
}

func NewEntryBuilder() *EntryBuilder {
	return &EntryBuilder{entry: &cacheEntry{}}
}
