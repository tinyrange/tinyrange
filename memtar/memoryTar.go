package memtar

import (
	"archive/tar"
	"bytes"
	"cmp"
	"encoding/gob"
	"errors"
	"io"
	"io/fs"
	"strings"
	"time"

	"golang.org/x/exp/slices"
)

type Header interface {
	Header() *tar.Header

	Typeflag() byte
	Name() string
	Linkname() string
	Size() int64
	Mode() int64
	FileMode() fs.FileMode
	Uid() int
	Gid() int
	ModTime() time.Time
}

type tarHeader struct {
	hdr *tar.Header
}

func (e *tarHeader) Typeflag() byte        { return e.hdr.Typeflag }
func (e *tarHeader) Name() string          { return e.hdr.Name }
func (e *tarHeader) Linkname() string      { return e.hdr.Linkname }
func (e *tarHeader) Size() int64           { return e.hdr.Size }
func (e *tarHeader) Mode() int64           { return e.hdr.Mode }
func (e *tarHeader) FileMode() fs.FileMode { return e.hdr.FileInfo().Mode() }
func (e *tarHeader) Uid() int              { return e.hdr.Uid }
func (e *tarHeader) Gid() int              { return e.hdr.Gid }
func (e *tarHeader) ModTime() time.Time    { return e.hdr.ModTime }

func (e *tarHeader) Header() *tar.Header {
	return &tar.Header{
		Typeflag:   e.Typeflag(),
		Name:       e.hdr.Name,
		Linkname:   e.hdr.Linkname,
		Size:       e.hdr.Size,
		Mode:       e.hdr.Mode,
		Uid:        e.hdr.Uid,
		Gid:        e.hdr.Gid,
		ModTime:    e.hdr.ModTime,
		AccessTime: e.hdr.AccessTime,
		ChangeTime: e.hdr.ChangeTime,
		Devmajor:   e.hdr.Devmajor,
		Devminor:   e.hdr.Devminor,
	}
}

var (
	_ Header = &tarHeader{}
)

func HeaderFromTarHeader(hdr *tar.Header) Header {
	return &tarHeader{hdr: hdr}
}

type Entry interface {
	Header
	Filename() string
	Open() io.Reader
}

type CacheEntry struct {
	underlyingFile io.ReaderAt

	HTypeflag byte      `json:"t"`
	HName     string    `json:"n"`
	HLinkname string    `json:"l"`
	HSize     int64     `json:"s"`
	HMode     int64     `json:"m"`
	HUid      int       `json:"u"`
	HGid      int       `json:"g"`
	HModTime  time.Time `json:"mt"`
	HDevMajor int64     `json:"da"`
	HDevMinor int64     `json:"di"`

	HFilename string `json:"f"`
	Offset    int64  `json:"o"`
}

// Filename implements Entry.
func (e *CacheEntry) Filename() string {
	return e.HFilename
}

// Open implements Entry.
func (e *CacheEntry) Open() io.Reader {
	return io.NewSectionReader(e.underlyingFile, e.Offset, e.HSize)
}

func (e *CacheEntry) Typeflag() byte        { return e.HTypeflag }
func (e *CacheEntry) Name() string          { return e.HName }
func (e *CacheEntry) Linkname() string      { return e.HLinkname }
func (e *CacheEntry) Size() int64           { return e.HSize }
func (e *CacheEntry) Mode() int64           { return e.HMode }
func (e *CacheEntry) FileMode() fs.FileMode { return e.Header().FileInfo().Mode() }
func (e *CacheEntry) Uid() int              { return e.HUid }
func (e *CacheEntry) Gid() int              { return e.HGid }
func (e *CacheEntry) ModTime() time.Time    { return e.HModTime }

func (e *CacheEntry) Header() *tar.Header {
	return &tar.Header{
		Typeflag: e.Typeflag(),
		Name:     e.HName,
		Linkname: e.HLinkname,
		Size:     e.HSize,
		Mode:     e.HMode,
		Uid:      e.HUid,
		Gid:      e.HGid,
		ModTime:  e.HModTime,
		Devmajor: e.HDevMajor,
		Devminor: e.HDevMinor,
	}
}

var (
	_ Entry = &CacheEntry{}
)

type optimizedEntry struct {
	tarHeader
	filename       string
	underlyingFile io.ReaderAt
	offset         int64
}

// Filename implements Entry.
func (e *optimizedEntry) Filename() string {
	return e.filename
}

// Open implements Entry.
func (e *optimizedEntry) Open() io.Reader {
	return io.NewSectionReader(e.underlyingFile, e.offset, e.hdr.Size)
}

type memoryEntry struct {
	tarHeader
	filename string
	body     []byte
}

func (e *memoryEntry) Filename() string {
	return e.filename
}

func (e *memoryEntry) Open() io.Reader {
	return bytes.NewReader(e.body)
}

var (
	_ Entry = &optimizedEntry{}
	_ Entry = &memoryEntry{}
)

type TarReader interface {
	Entries() []Entry
}

type MemoryTarReader struct {
	entryMap map[string]Entry
}

func (m *MemoryTarReader) Entries() []Entry {
	var entries []Entry

	for _, v := range m.entryMap {
		entries = append(entries, v)
	}

	slices.SortFunc(entries, func(i, j Entry) int {
		fileI := i.Filename()
		fileJ := j.Filename()

		if i.Typeflag() == tar.TypeDir && !strings.HasSuffix(fileI, "/") {
			fileI += "/"
		}

		if j.Typeflag() == tar.TypeDir && !strings.HasSuffix(fileJ, "/") {
			fileJ += "/"
		}

		return cmp.Compare(fileI, fileJ)
	})

	return entries
}

type SeekableReader interface {
	io.Reader
	io.Seeker
	io.ReaderAt
}

func (m *MemoryTarReader) ReadSeekableTar(r SeekableReader) error {
	tarReader := tar.NewReader(r)

	for {
		hdr, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		off, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}

		name := strings.TrimPrefix(hdr.Name, "/")

		if hdr.Typeflag == tar.TypeDir && !strings.HasSuffix(name, "/") {
			name += "/"
		}

		m.entryMap[name] = &optimizedEntry{
			filename:       name,
			tarHeader:      tarHeader{hdr: hdr},
			underlyingFile: r,
			offset:         off,
		}
	}

	return nil
}

func (m *MemoryTarReader) ReadTar(r io.Reader) error {
	tarReader := tar.NewReader(r)

	for {
		hdr, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		// Decompress all files and read them into memory.
		body, err := io.ReadAll(tarReader)
		if err != nil {
			return err
		}

		m.entryMap[hdr.Name] = &memoryEntry{
			tarHeader: tarHeader{hdr: hdr},
			filename:  hdr.Name,
			body:      body,
		}
	}

	return nil
}

func (m *MemoryTarReader) AddEntries(r io.Reader) error {
	if readerAt, ok := r.(SeekableReader); ok {
		return m.ReadSeekableTar(readerAt)
	} else {
		return m.ReadTar(r)
	}
}

func (m *MemoryTarReader) CopyFrom(r TarReader) error {
	for _, ent := range r.Entries() {
		m.entryMap[ent.Name()] = ent
	}

	return nil
}

func NewReader() *MemoryTarReader {
	return &MemoryTarReader{entryMap: make(map[string]Entry)}
}

func ReadTarIntoMemory(r io.Reader) (*MemoryTarReader, error) {
	memTar := NewReader()

	if err := memTar.AddEntries(r); err != nil {
		return nil, err
	}

	return memTar, nil
}

func CreateTarIndex(r SeekableReader, w io.Writer) (TarReader, error) {
	memTar := NewReader()

	if err := memTar.ReadSeekableTar(r); err != nil {
		return nil, err
	}

	var ret []CacheEntry

	for _, ent := range memTar.Entries() {
		optEnt := ent.(*optimizedEntry)

		ret = append(ret, CacheEntry{
			HTypeflag: optEnt.hdr.Typeflag,
			HName:     optEnt.hdr.Name,
			HLinkname: optEnt.hdr.Linkname,
			HSize:     optEnt.hdr.Size,
			HMode:     optEnt.hdr.Mode,
			HUid:      optEnt.hdr.Uid,
			HGid:      optEnt.hdr.Gid,
			HModTime:  optEnt.hdr.ModTime,
			HDevMajor: optEnt.hdr.Devmajor,
			HDevMinor: optEnt.hdr.Devminor,

			HFilename: optEnt.filename,
			Offset:    optEnt.offset,
		})
	}

	enc := gob.NewEncoder(w)

	if err := enc.Encode(ret); err != nil {
		return nil, err
	}

	return memTar, nil
}

type IndexedReader struct {
	underlying SeekableReader
	entries    []CacheEntry
}

// Entries implements TarReader.
func (i *IndexedReader) Entries() []Entry {
	var ret []Entry

	for _, ent := range i.entries {
		ent.underlyingFile = i.underlying
		ret = append(ret, &ent)
	}

	return ret
}

var (
	_ TarReader = &IndexedReader{}
)

func LoadTarIndex(underlying SeekableReader, cache io.Reader) (TarReader, error) {
	ret := &IndexedReader{underlying: underlying}

	dec := gob.NewDecoder(cache)

	if err := dec.Decode(&ret.entries); err != nil {
		return nil, err
	}

	return ret, nil
}
