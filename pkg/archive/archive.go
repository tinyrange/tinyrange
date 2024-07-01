package archive

import (
	"encoding/json"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/tinyrange/vm"
)

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

type statInterface struct {
	io.ReaderAt
	size int64
}

// IsDir implements fs.FileInfo.
func (s *statInterface) IsDir() bool {
	panic("unimplemented")
}

// ModTime implements fs.FileInfo.
func (s *statInterface) ModTime() time.Time {
	panic("unimplemented")
}

// Mode implements fs.FileInfo.
func (s *statInterface) Mode() fs.FileMode {
	panic("unimplemented")
}

// Name implements fs.FileInfo.
func (s *statInterface) Name() string {
	panic("unimplemented")
}

// Size implements fs.FileInfo.
func (s *statInterface) Size() int64 {
	return s.size
}

// Sys implements fs.FileInfo.
func (s *statInterface) Sys() any {
	panic("unimplemented")
}

// Stat implements vm.File.
func (s *statInterface) Stat() (fs.FileInfo, error) {
	return s, nil
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

// Open implements Entry.
func (e *CacheEntry) Open() (vm.File, error) {
	return &statInterface{ReaderAt: io.NewSectionReader(e.underlyingFile, e.COffset, e.CSize), size: e.CSize}, nil
}

func ReadArchiveFromFile(fh io.ReaderAt) ([]CacheEntry, error) {
	var off int64 = 0

	var ret []CacheEntry

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

		ret = append(ret, hdr)

		off += hdr.CSize
	}

	return ret, nil
}
