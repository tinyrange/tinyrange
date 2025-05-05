package fsutil

import (
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/archive2"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/path"
)

func ExtractArchive2ToFilesystem(ark *archive2.ArchiveReader, ext filesystem.ExtendedFileMethods, target string, dir filesystem.MutableDirectory, deletedFiles map[string]bool) error {
	for {
		err := ark.NextEntry()
		if err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("failed to read entry: %w", err)
		}

		ent := ark

		// TODO(joshua): Why is this not path.Native.Join?
		name := target + "/" + ent.Name()

		var file filesystem.MutableFile

		if name != "/" {
			if Exists(dir, name) {
				continue
			}

			dirname := path.Unix.Dir(name)

			if !Exists(dir, dirname) && path.Unix.Clean(name) != dirname {
				// log.Info("mkdir", "dirname", dirname)
				if _, err := Mkdir(dir, dirname); err != nil {
					return err
				}
			}

			switch ent.Kind() {
			case archive2.EntryKindDirectory:
				// log.Info("directory", "name", name)
				name = strings.TrimSuffix(name, "/")

				file, err = Mkdir(dir, name)
				if err != nil {
					return err
				}
			case archive2.EntryKindSymlink:
				// log.Info("symlink", "name", name)
				symlink := filesystem.NewSymlink(ent.Linkname())

				file = symlink

				if _, err := CreateChild(dir, name, symlink); err != nil {
					return err
				}
			case archive2.EntryKindHardlink:
				// log.Info("link", "name", name, "target", ent.Linkname())
				link, err := filesystem.NewHardLink(ent.Linkname())
				if err != nil {
					return err
				}

				file = link

				if _, err := CreateChild(dir, name, link); err != nil {
					return err
				}
			case archive2.EntryKindRegular:
				f, err := ent.File(ext)
				if err != nil {
					return fmt.Errorf("failed to get file: %w", err)
				}

				if _, err := CreateChild(dir, name, f); err != nil {
					return err
				}
			case archive2.EntryKindExtended:
				f, err := ent.File(ext)
				if err != nil {
					return fmt.Errorf("failed to get file: %w", err)
				}

				if _, err := CreateChild(dir, name, f); err != nil {
					return err
				}
			case archive2.EntryKindDeleted:
				if err := DeleteChild(dir, name); err != nil {
					return err
				}

				if deletedFiles != nil {
					deletedFiles[name] = true
				}
			default:
				return fmt.Errorf("unimplemented entry type: %s", ent.Kind())
			}
		} else {
			file = dir
		}

		if file != nil {
			uid, gid := ent.Owner()

			if err := file.Chown(uid, gid); err != nil {
				return fmt.Errorf("failed to chown in guest: %w", err)
			}

			if err := file.Chmod(fs.FileMode(ent.Mode())); err != nil {
				return fmt.Errorf("failed to chmod in guest: %w", err)
			}

			if err := file.Chtimes(ent.ModTime()); err != nil {
				return fmt.Errorf("failed to set modtime in guest: %w", err)
			}
		}
	}

	return nil
}
