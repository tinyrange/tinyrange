package filesystem

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/hash"
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

func ReadArchiveFromFile(f File) (Archive, error) {
	fh, err := f.Open()
	if err != nil {
		return nil, err
	}

	var source hash.SerializableValue

	if src, err := SourceFromFile(f); err == nil {
		source = src
	}

	var ret ArrayArchive

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

		var hdr CacheEntry

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

func ExtractArchive(ark Archive, mut MutableDirectory) error {
	ents, err := ark.Entries()
	if err != nil {
		return err
	}

	for _, ent := range ents {
		if err := ExtractEntry(ent, mut); err != nil {
			return err
		}
	}

	return nil
}

func ArchiveFromFS(eFs embed.FS, base string) (ArrayArchive, error) {
	var ents ArrayArchive

	if err := fs.WalkDir(eFs, base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.IsDir() {
			ents = append(ents, SimpleEntry{
				File:     NewMemoryDirectory(),
				mode:     info.Mode(),
				name:     path,
				size:     info.Size(),
				typeFlag: TypeDirectory,
			})
		} else {
			f, err := eFs.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()

			contents, err := io.ReadAll(f)
			if err != nil {
				return err
			}

			mf := NewMemoryFile(TypeRegular)
			mf.Overwrite(contents)

			ents = append(ents, SimpleEntry{
				File:     mf,
				mode:     info.Mode(),
				name:     path,
				size:     info.Size(),
				typeFlag: TypeRegular,
			})
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return ents, nil
}

const CACHE_ENTRY_SIZE = 1024

type CacheEntry struct {
	underlyingFile   io.ReaderAt
	underlyingSource hash.SerializableValue

	COffset   int64    `json:"o"`
	CTypeflag FileType `json:"t"`
	CName     string   `json:"n"`
	CLinkname string   `json:"l"`
	CSize     int64    `json:"s"`
	CMode     int64    `json:"m"`
	CUid      int      `json:"u"`
	CGid      int      `json:"g"`
	CModTime  int64    `json:"e"` // in microseconds since the unix epoch.
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
	if e.CTypeflag != TypeRegular {
		return nil, fmt.Errorf("file is not a regular file: %s", e.CTypeflag.String())
	}
	return NewNopCloserFileHandle(
		io.NewSectionReader(e.underlyingFile, e.COffset, e.CSize),
	), nil
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

type ArchiveWriter struct {
	w      io.Writer
	offset int64
}

func (w *ArchiveWriter) WriteEntry(ent *CacheEntry, r io.Reader) error {
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
