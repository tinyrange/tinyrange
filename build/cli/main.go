package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tinyrange/tinyrange/build"
	"github.com/tinyrange/tinyrange/build/proto"
)

func Main() error {
	fs := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)

	fs.Parse(os.Args[1:])

	db, err := build.NewDatabase()
	if err != nil {
		return err
	}

	fact := db.Factory()

	art, err := db.Build(
		fact.NewExtractArchive(
			fact.NewFetchHttp("https://example.com/archive.tar.gz"),
			proto.ArchiveType_TAR,
			proto.CompressionType_GZIP,
		),
	)
	if err != nil {
		return err
	}

	_ = art

	return fmt.Errorf("not implemented")
}
