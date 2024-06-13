package filesystem

import (
	"encoding/json"
	"io"
	"strings"
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
