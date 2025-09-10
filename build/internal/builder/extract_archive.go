package builder

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"

	"github.com/tinyrange/tinyrange/archive"
	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/internal/registry"
	"github.com/tinyrange/tinyrange/build/proto"
)

type extractArchiveBuilder struct {
}

// Build implements common.Builder.
func (e *extractArchiveBuilder) Build(ctx common.Context) error {
	var params proto.ExtractArchiveDefinition
	if err := ctx.Decode(&params); err != nil {
		return err
	}

	in, err := ReaderFromSource(ctx, params.Source)
	if err != nil {
		return err
	}
	defer in.Close()

	var reader io.ReadCloser

	switch params.CompressionType {
	case proto.CompressionType_COMPRESSION_TYPE_NONE:
		reader = in
	case proto.CompressionType_COMPRESSION_TYPE_GZIP:
		var err error
		reader, err = gzip.NewReader(in)
		if err != nil {
			return err
		}
		defer reader.Close()
	default:
		return fmt.Errorf("unsupported compression type: %v", params.CompressionType)
	}

	ark, err := ctx.CreateArchive()
	if err != nil {
		return err
	}
	defer ark.Close()

	switch params.ArchiveType {
	case proto.ArchiveType_ARCHIVE_TYPE_TAR:
		r := tar.NewReader(reader)

		for {
			hdr, err := r.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			info := hdr.FileInfo()

			var typeFlag archive.EntryKind

			switch hdr.Typeflag {
			case tar.TypeReg:
				typeFlag = archive.EntryKindRegular
			case tar.TypeDir:
				typeFlag = archive.EntryKindDirectory
			case tar.TypeChar:
				// TODO(joshua): Handle character devices.
				continue
			case tar.TypeBlock:
				// TODO(joshua): Handle block devices.
				continue
			case tar.TypeSymlink:
				typeFlag = archive.EntryKindSymlink
			case tar.TypeLink:
				typeFlag = archive.EntryKindHardlink
			case tar.TypeXGlobalHeader:
				continue
			default:
				return fmt.Errorf("unknown type flag: %d", hdr.Typeflag)
			}

			var fact archive.EntryFactory

			if err := ark.WriteEntry(fact.
				Kind(typeFlag).
				Name(hdr.Name).
				Linkname(hdr.Linkname).
				Size(hdr.Size).
				Mode(info.Mode()).
				Owner(hdr.Uid, hdr.Gid).
				ModTime(hdr.ModTime), r); err != nil {
				return err
			}
		}

		return nil
	default:
		return fmt.Errorf("unsupported archive type: %v", params.ArchiveType)
	}
}

func init() {
	registry.Register(
		common.TYPE_NAME_EXTRACT_ARCHIVE,
		&proto.ExtractArchiveDefinition{},
		&extractArchiveBuilder{},
	)
}
