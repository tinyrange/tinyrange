package filesystem

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"strings"

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
