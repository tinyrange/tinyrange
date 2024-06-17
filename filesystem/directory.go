package filesystem

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

type DirectoryEntry struct {
	File
	Name string
}

type Directory interface {
	File

	GetChild(name string) (DirectoryEntry, error)
	Readdir() ([]DirectoryEntry, error)
}

func getMutable(dir Directory) MutableDirectory {
	if mut, ok := dir.(MutableDirectory); ok {
		return mut
	} else if mut, ok := dir.(*StarDirectory); ok {
		return getMutable(mut.Directory)
	} else {
		return nil
	}
}

func OpenPath(dir Directory, p string) (DirectoryEntry, error) {
	tokens := strings.Split(path.Clean(p), "/")

	var currentDir = dir

	for _, token := range tokens[:len(tokens)-1] {
		child, err := currentDir.GetChild(token)
		if err != nil {
			return DirectoryEntry{}, err
		}

		childDir, ok := child.File.(Directory)
		if !ok {
			return DirectoryEntry{}, fmt.Errorf("child %T is not a directory", child.File)
		}

		currentDir = childDir
	}

	return currentDir.GetChild(tokens[len(tokens)-1])
}

func Mkdir(dir Directory, p string) (MutableDirectory, error) {
	tokens := strings.Split(path.Clean(p), "/")

	var currentDir = dir

	for _, token := range tokens[:len(tokens)-1] {
		child, err := currentDir.GetChild(token)
		if err == fs.ErrNotExist {
			if mut := getMutable(currentDir); mut != nil {
				newChild, err := mut.Mkdir(token)
				if err != nil {
					return nil, err
				}

				child = DirectoryEntry{File: newChild, Name: token}
			} else {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}

		childDir, ok := child.File.(Directory)
		if !ok {
			return nil, fmt.Errorf("child %T is not a directory", child.File)
		}

		currentDir = childDir
	}

	mut := getMutable(currentDir)
	if mut == nil {
		return nil, fmt.Errorf("directory %T is not mutable", currentDir)
	}

	return mut.Mkdir(tokens[len(tokens)-1])
}

func CreateChild(dir Directory, p string, f File) error {
	tokens := strings.Split(path.Clean(p), "/")

	var currentDir = dir

	for _, token := range tokens[:len(tokens)-1] {
		child, err := currentDir.GetChild(token)
		if err == fs.ErrNotExist {
			if mut := getMutable(currentDir); mut != nil {
				newChild, err := mut.Mkdir(token)
				if err != nil {
					return err
				}

				child = DirectoryEntry{File: newChild, Name: token}
			} else {
				return err
			}
		} else if err != nil {
			return err
		}

		childDir, ok := child.File.(Directory)
		if !ok {
			return fmt.Errorf("child %T is not a directory", child.File)
		}

		currentDir = childDir
	}

	mut := getMutable(currentDir)
	if mut == nil {
		return fmt.Errorf("directory %T is not mutable", currentDir)
	}

	return mut.Create(tokens[len(tokens)-1], f)
}

type MutableDirectory interface {
	Directory
	MutableFile

	Mkdir(name string) (MutableDirectory, error)
	Create(name string, f File) error
}

type memoryDirectory struct {
	*memoryFile

	entries map[string]File
}

// IsDir implements FileInfo.
func (m *memoryDirectory) IsDir() bool {
	return true
}

// Sys implements FileInfo.
func (m *memoryDirectory) Sys() any {
	return m
}

// Create implements MutableDirectory.
func (m *memoryDirectory) Create(name string, f File) error {
	if strings.Contains(name, "/") {
		return fmt.Errorf("MutableDirectory methods can not handle paths")
	}

	m.entries[name] = f

	return nil
}

// GetChild implements MutableDirectory.
func (m *memoryDirectory) GetChild(name string) (DirectoryEntry, error) {
	if strings.Contains(name, "/") {
		return DirectoryEntry{}, fmt.Errorf("MutableDirectory methods can not handle paths")
	}

	child, ok := m.entries[name]
	if !ok {
		return DirectoryEntry{}, fs.ErrNotExist
	}

	return DirectoryEntry{File: child, Name: name}, nil
}

// Mkdir implements MutableDirectory.
func (m *memoryDirectory) Mkdir(name string) (MutableDirectory, error) {
	if strings.Contains(name, "/") {
		return nil, fmt.Errorf("MutableDirectory methods can not handle paths")
	}

	child := NewMemoryDirectory()

	m.entries[name] = child

	return child, nil
}

// Open implements MutableDirectory.
func (m *memoryDirectory) Open() (FileHandle, error) {
	return nil, fs.ErrInvalid
}

// Overwrite implements MutableDirectory.
func (m *memoryDirectory) Overwrite(contents []byte) error {
	return fs.ErrInvalid
}

// Readdir implements MutableDirectory.
func (m *memoryDirectory) Readdir() ([]DirectoryEntry, error) {
	var ret []DirectoryEntry

	for name, file := range m.entries {
		ret = append(ret, DirectoryEntry{File: file, Name: name})
	}

	return ret, nil
}

// Stat implements MutableDirectory.
func (m *memoryDirectory) Stat() (FileInfo, error) {
	return m, nil
}

var (
	_ MutableDirectory = &memoryDirectory{}
)

func NewMemoryDirectory() MutableDirectory {
	return &memoryDirectory{
		memoryFile: NewMemoryFile().(*memoryFile),
		entries:    make(map[string]File),
	}
}

func ExtractEntry(ent Entry, dir MutableDirectory) error {
	switch ent.Typeflag() {
	case TypeDirectory:
		child, err := Mkdir(dir, ent.Name())
		if err != nil {
			return err
		}

		if err := child.Chmod(ent.Mode()); err != nil {
			return err
		}

		if err := child.Chown(ent.Uid(), ent.Gid()); err != nil {
			return err
		}

		if err := child.Chtimes(ent.ModTime()); err != nil {
			return err
		}

		return nil
	case TypeRegular:
		return CreateChild(dir, ent.Name(), ent)
	default:
		return fmt.Errorf("unknown Entry type: %s", ent.Typeflag())
	}
}
