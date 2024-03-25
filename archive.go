package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/tinyrange/pkg2/memtar"
)

func ReadArchive(r io.Reader, ext string) (memtar.TarReader, error) {
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
	} else if strings.HasSuffix(ext, ".zst") {
		ext = strings.TrimSuffix(ext, ".zst")

		reader, err = zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
	}

	if strings.HasSuffix(ext, ".tar") {
		t := memtar.NewReader()

		if err := t.AddEntries(reader); err != nil {
			return nil, err
		}

		return t, nil
	} else {
		return nil, fmt.Errorf("unknown extension: %s", ext)
	}
}
