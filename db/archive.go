package db

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/blakesmith/ar"
	"github.com/cavaliergopher/cpio"
	"github.com/klauspost/compress/zstd"
	"github.com/tinyrange/pkg2/memtar"
	"github.com/xi2/xz"
)

type entryList []memtar.Entry

// Entries implements memtar.TarReader.
func (e entryList) Entries() []memtar.Entry { return e }

var (
	_ memtar.TarReader = &entryList{}
)

type modifiedEntry struct {
	memtar.Entry
	filename string
}

func (ent modifiedEntry) Filename() string {
	return ent.filename
}

func stripTarComponents(archive memtar.TarReader, count int) (memtar.TarReader, error) {
	var ret entryList

	for _, ent := range archive.Entries() {
		name := ent.Filename()
		components := strings.Split(name, "/")
		if len(components) <= count {
			continue
		}
		ret = append(ret, &modifiedEntry{Entry: ent, filename: strings.Join(components[count:], "/")})
	}

	return ret, nil
}

func ReadArchive(r io.Reader, ext string, stripComponents int) (memtar.TarReader, error) {
	var (
		reader io.Reader
		err    error
	)

	if strings.HasSuffix(ext, ".gz") {
		ext = strings.TrimSuffix(ext, ".gz")

		reader, err = gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
	} else if strings.HasSuffix(ext, ".xz") {
		ext = strings.TrimSuffix(ext, ".xz")

		reader, err = xz.NewReader(r, 0)
		if err != nil {
			return nil, err
		}
	} else if strings.HasSuffix(ext, ".zst") {
		ext = strings.TrimSuffix(ext, ".zst")

		reader, err = zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
	} else if strings.HasSuffix(ext, ".bz2") {
		ext = strings.TrimSuffix(ext, ".bz2")

		reader = bzip2.NewReader(r)
	} else {
		reader = r
	}

	if strings.HasSuffix(ext, ".tar") {
		t := memtar.NewReader()

		if err := t.AddEntries(reader); err != nil {
			return nil, err
		}

		if stripComponents != 0 {
			return stripTarComponents(t, stripComponents)
		} else {
			return t, nil
		}
	} else if strings.HasSuffix(ext, ".ar") {
		reader := ar.NewReader(reader)

		var ret memtar.ArrayReader

		for {
			hdr, err := reader.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				return nil, err
			}

			memHdr := memtar.HeaderFromTarHeader(&tar.Header{
				Name:    hdr.Name,
				ModTime: hdr.ModTime,
				Size:    hdr.Size,
				Mode:    hdr.Mode,
				Uid:     hdr.Uid,
				Gid:     hdr.Gid,
			})

			body, err := io.ReadAll(reader)
			if err != nil {
				return nil, err
			}

			ent := &memtar.MemoryEntry{
				Header:      memHdr,
				EntFilename: hdr.Name,
				Body:        body,
			}

			ret = append(ret, ent)
		}

		return ret, nil
	} else if strings.HasSuffix(ext, ".cpio") {
		reader := cpio.NewReader(reader)

		var ret memtar.ArrayReader

		for {
			hdr, err := reader.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				return nil, err
			}

			fileInfo := hdr.FileInfo()

			tarHdr, err := tar.FileInfoHeader(fileInfo, hdr.Linkname)
			if err != nil {
				return nil, err
			}

			tarHdr.Name = hdr.Name
			if fileInfo.IsDir() {
				tarHdr.Name += "/"
			}

			memHdr := memtar.HeaderFromTarHeader(tarHdr)

			body, err := io.ReadAll(reader)
			if err != nil {
				return nil, err
			}

			ent := &memtar.MemoryEntry{
				Header:      memHdr,
				EntFilename: tarHdr.Name,
				Body:        body,
			}

			ret = append(ret, ent)
		}

		return ret, nil
	} else {
		return nil, fmt.Errorf("unknown extension: %s", ext)
	}
}
