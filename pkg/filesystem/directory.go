package filesystem

import (
	"fmt"
	"io/fs"
	"sync"

	"github.com/tinyrange/tinyrange/pkg/path"
)

func GetMutableDirectory(dir Directory) MutableDirectory {
	switch dir := dir.(type) {
	case AsMutableDirectory:
		return dir.AsMutableDirectory()
	case MutableDirectory:
		return dir
	default:
		return nil
	}
}

type AsMutableDirectory interface {
	AsMutableDirectory() MutableDirectory
}

type memoryDirectory struct {
	*memoryFile

	mtx     sync.RWMutex
	names   []string
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

// Unlink implements MutableDirectory.
func (m *memoryDirectory) Unlink(name string) error {
    m.mtx.Lock()
    defer m.mtx.Unlock()

	if path.Unix.Base(name) != name {
		return fmt.Errorf("MutableDirectory methods can not handle paths: %s", name)
	}

    // Remove from map
    delete(m.entries, name)
    // Remove from ordered names slice
    for i, n := range m.names {
        if n == name {
            // compact without preserving order strictly
            m.names = append(m.names[:i], m.names[i+1:]...)
            break
        }
    }

    return nil
}

// Create implements MutableDirectory.
func (m *memoryDirectory) Create(name string, f File) (File, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	newFile, err := m.create(name, f)
	if err != nil {
		return nil, err
	}

	return newFile, nil
}

func (m *memoryDirectory) create(name string, f File) (File, error) {
	if name == "" || name == "." {
		return nil, fmt.Errorf("invalid name specified for child: %s", name)
	}

	if path.Unix.Base(name) != name {
		return nil, fmt.Errorf("MutableDirectory methods can not handle paths: %s", name)
	}

	if f == nil {
		f = m.fac.NewMemoryFile()
	}

    // If an entry already exists, replace it and avoid duplicate names entries.
    if _, exists := m.entries[name]; exists {
        // Remove first occurrence from names to avoid duplicates.
        for i, n := range m.names {
            if n == name {
                m.names = append(m.names[:i], m.names[i+1:]...)
                break
            }
        }
    }
    m.names = append(m.names, name)
    m.entries[name] = f

	return m.entries[name], nil
}

// GetChild implements MutableDirectory.
func (m *memoryDirectory) GetChild(name string) (DirectoryEntry, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	if name == "" || name == "." {
		return DirectoryEntry{File: m}, nil
	}

	if path.Unix.Base(name) != name {
		return DirectoryEntry{}, fmt.Errorf("MutableDirectory methods can not handle paths: %s", name)
	}

	child, ok := m.entries[name]
	if !ok {
		return DirectoryEntry{}, fs.ErrNotExist
	}

	return DirectoryEntry{File: child, Name: name}, nil
}

// Mkdir implements MutableDirectory.
func (m *memoryDirectory) Mkdir(name string) (MutableDirectory, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if name == "" || name == "." {
		return nil, fmt.Errorf("invalid name specified for child: %s", name)
	}

	if path.Unix.Base(name) != name {
		return nil, fmt.Errorf("MutableDirectory methods can not handle paths: %s", name)
	}

	if ent, exists := m.entries[name]; exists {
		if dir, ok := ent.(Directory); ok {
			mut := GetMutableDirectory(dir)
			if mut != nil {
				return mut, nil
			} else {
				return nil, fmt.Errorf("child is not mutable: %T", dir)
			}
		} else {
			return nil, fmt.Errorf("entry is not a directory: %T", ent)
		}
	}

	newChild, err := m.create(name, m.fac.NewMemoryDirectory())
	if err != nil {
		return nil, err
	}

	if newChild == nil {
		mut, ok := m.entries[name].(MutableDirectory)
		if !ok {
			return nil, fmt.Errorf("child is not mutable: %T", m.entries[name])
		}

		return mut, nil
	}

	if mut, ok := newChild.(MutableDirectory); ok {
		return mut, nil
	} else {
		return nil, fmt.Errorf("child is not mutable: %T", newChild)
	}
}

// Open implements MutableDirectory.
func (m *memoryDirectory) Open() (FileHandle, error) {
	return nil, fs.ErrInvalid
}

// Overwrite implements MutableDirectory.
func (m *memoryDirectory) Overwrite(contents []byte) error {
	return fs.ErrInvalid
}

// Truncate implements MutableDirectory.
func (m *memoryDirectory) Truncate(size int64) error {
	return fs.ErrInvalid
}

// Readdir implements MutableDirectory.
func (m *memoryDirectory) Readdir() ([]DirectoryEntry, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	var ret []DirectoryEntry

	for _, name := range m.names {
		file := m.entries[name]
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
