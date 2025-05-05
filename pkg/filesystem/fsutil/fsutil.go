package fsutil

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/path"
)

func Exists(dir filesystem.Directory, p string) bool {
	_, err := OpenPath(dir, p)
	return err == nil
}

func resolveDirectory(root filesystem.Directory, file filesystem.File, name string) (filesystem.Directory, error) {
	if dir, ok := file.(filesystem.Directory); ok {
		return dir, nil
	}

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	switch info.Kind() {
	case filesystem.TypeSymlink:
		target, err := filesystem.GetLinkName(file)
		if err != nil {
			return nil, err
		}

		currentDir := path.Unix.Dir(name)

		newTarget := path.Unix.Join(currentDir, target)

		ent, err := OpenPath(root, newTarget)
		if err != nil {
			return nil, err
		}

		return resolveDirectory(root, ent.File, newTarget)
	default:
		return nil, fmt.Errorf("OpenPath(%s): child %T is not a directory (kind=%s)", name, file, info.Kind())
	}
}

func OpenPath(dir filesystem.Directory, p string) (filesystem.DirectoryEntry, error) {
	p = strings.TrimPrefix(p, "/")

	tokens := strings.Split(path.Unix.Clean(p), "/")

	var currentDir = dir

	for i, token := range tokens[:len(tokens)-1] {
		child, err := currentDir.GetChild(token)
		if err != nil {
			return filesystem.DirectoryEntry{}, err
		}

		childDir, err := resolveDirectory(dir, child.File, path.Unix.Join(tokens[:i+1]...))
		if err != nil {
			return filesystem.DirectoryEntry{}, err
		}

		currentDir = childDir
	}

	dirname := tokens[len(tokens)-1]

	if dirname == "." {
		return filesystem.DirectoryEntry{
			File: currentDir,
			Name: ".",
		}, nil
	}

	return currentDir.GetChild(dirname)
}

func Mkdir(dir filesystem.Directory, p string) (filesystem.MutableDirectory, error) {
	p = strings.TrimPrefix(p, "/")

	tokens := strings.Split(path.Unix.Clean(p), "/")

	var currentDir = dir

	for i, token := range tokens[:len(tokens)-1] {
		child, err := currentDir.GetChild(token)
		if err == fs.ErrNotExist {
			if mut := filesystem.GetMutableDirectory(currentDir); mut != nil {
				newChild, err := mut.Mkdir(token)
				if err != nil {
					return nil, err
				}

				child = filesystem.DirectoryEntry{File: newChild, Name: token}
			} else {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}

		childDir, err := resolveDirectory(dir, child.File, path.Unix.Join(tokens[:i+1]...))
		if err != nil {
			return nil, err
		}

		currentDir = childDir
	}

	mut := filesystem.GetMutableDirectory(currentDir)
	if mut == nil {
		return nil, fmt.Errorf("directory %T is not mutable", currentDir)
	}

	dirname := tokens[len(tokens)-1]

	if dirname == "." {
		return mut, nil
	}

	return mut.Mkdir(dirname)
}

func CreateChild(dir filesystem.Directory, p string, f filesystem.File) (filesystem.File, error) {
	p = strings.TrimPrefix(p, "/")

	tokens := strings.Split(path.Unix.Clean(p), "/")

	var currentDir = dir

	for i, token := range tokens[:len(tokens)-1] {
		child, err := currentDir.GetChild(token)
		if err == fs.ErrNotExist {
			if mut := filesystem.GetMutableDirectory(currentDir); mut != nil {
				newChild, err := mut.Mkdir(token)
				if err != nil {
					return nil, err
				}

				child = filesystem.DirectoryEntry{File: newChild, Name: token}
			} else {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}

		childDir, err := resolveDirectory(dir, child.File, path.Unix.Join(tokens[:i+1]...))
		if err != nil {
			return nil, err
		}

		currentDir = childDir
	}

	mut := filesystem.GetMutableDirectory(currentDir)
	if mut == nil {
		return nil, fmt.Errorf("directory %T is not mutable", currentDir)
	}

	return mut.Create(tokens[len(tokens)-1], f)
}

func DeleteChild(dir filesystem.Directory, p string) error {
	p = strings.TrimPrefix(p, "/")

	tokens := strings.Split(path.Unix.Clean(p), "/")

	var currentDir = dir

	for i, token := range tokens[:len(tokens)-1] {
		child, err := currentDir.GetChild(token)
		if err != nil {
			return err
		}

		childDir, err := resolveDirectory(dir, child.File, path.Unix.Join(tokens[:i+1]...))
		if err != nil {
			return err
		}

		currentDir = childDir
	}

	mut := filesystem.GetMutableDirectory(currentDir)
	if mut == nil {
		return fmt.Errorf("directory %T is not mutable", currentDir)
	}

	return mut.Unlink(tokens[len(tokens)-1])
}

func GetTotalSize(dir filesystem.Directory) (int64, error) {
	ents, err := dir.Readdir()
	if err != nil {
		return -1, err
	}

	var total int64 = 0

	for _, child := range ents {
		info, err := child.Stat()
		if err != nil {
			return -1, err
		}

		switch info.Kind() {
		case filesystem.TypeRegular:
			total += info.Size()
		case filesystem.TypeDirectory:
			dir, ok := child.File.(filesystem.Directory)
			if !ok {
				return -1, fmt.Errorf("child is not a directory %T", child.File)
			}

			childTotal, err := GetTotalSize(dir)
			if err != nil {
				return -1, err
			}

			total += childTotal
		default:
			continue
		}
	}

	return total, nil
}
