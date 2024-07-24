package common

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/filesystem/ext4"
	"github.com/tinyrange/vm"
)

func ExtractReaderTo(input io.Reader, kind string, fs *ext4.Ext4Filesystem, filter func(hdr *tar.Header) bool) error {
	var (
		reader io.Reader
		err    error
	)

	if strings.HasSuffix(kind, ".gz") {
		reader, err = gzip.NewReader(input)
		if err != nil {
			return err
		}

		kind = strings.TrimSuffix(kind, ".gz")
	} else {
		reader = input
	}

	if strings.HasSuffix(kind, ".tar") {
		tarReader := tar.NewReader(reader)

		for {
			hdr, err := tarReader.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				return err
			}

			if filter != nil && !filter(hdr) {
				continue
			}

			name := path.Clean(hdr.Name)

			if !fs.Exists(name) {
				switch hdr.Typeflag {
				case tar.TypeReg:
					contents, err := io.ReadAll(tarReader)
					if err != nil {
						return err
					}

					if err := fs.CreateFile(name, vm.RawRegion(contents)); err != nil {
						return err
					}
				case tar.TypeSymlink:
					if err := fs.Symlink(name, hdr.Linkname); err != nil {
						return err
					}
				case tar.TypeLink:
					if err := fs.Link(name, hdr.Linkname); err != nil {
						return err
					}
				case tar.TypeDir:
					if err := fs.Mkdir(name, false); err != nil {
						return err
					}
				default:
					return fmt.Errorf("Filesystem.AddFromTar: Typeflag not implemented: %d", hdr.Typeflag)
				}
			}

			info := hdr.FileInfo()

			if err := fs.Chmod(name, info.Mode()); err != nil {
				return err
			}
			if err := fs.Chown(name, uint16(hdr.Uid), uint16(hdr.Gid)); err != nil {
				return err
			}
			if err := fs.Chtimes(name, hdr.ModTime); err != nil {
				return err
			}
		}

		return nil
	} else {
		return fmt.Errorf("input has unknown archive format: %s", input)
	}
}

func ExtractArchiveTo(input string, fs *ext4.Ext4Filesystem) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	defer f.Close()

	return ExtractReaderTo(f, input, fs, func(hdr *tar.Header) bool { return true })
}
