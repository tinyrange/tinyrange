package filesystem

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/tinyrange/tinyrange/pkg/hash"
)

type osStat struct {
	fs.FileInfo
}

// Kind implements FileInfo.
func (o *osStat) Kind() FileType {
	if o.IsDir() {
		return TypeDirectory
	} else if o.Mode().Type() == fs.ModeSymlink {
		return TypeSymlink
	} else {
		return TypeRegular
	}
}

var (
	_ FileInfo = &osStat{}
)

type LocalFile struct {
	filename string
	source   hash.SerializableValue
}

// Digest implements File.
func (l *LocalFile) Digest() *FileDigest {
	return &FileDigest{Hash: l.filename}
}

// Open implements File.
func (l *LocalFile) Open() (FileHandle, error) {
	return os.OpenFile(l.filename, os.O_RDONLY, 0)
}

// Stat implements File.
func (l *LocalFile) Stat() (FileInfo, error) {
	s, err := os.Stat(l.filename)
	if err != nil {
		return nil, err
	}

	return &osStat{FileInfo: s}, nil
}

var (
	_ File = &LocalFile{}
)

func NewLocalFile(filename string, source hash.SerializableValue) File {
	return &LocalFile{filename: filename, source: source}
}

type LocalDirectory struct {
	*LocalFile
}

// GetChild implements Directory.
func (l *LocalDirectory) GetChild(name string) (DirectoryEntry, error) {
	if name == "" || name == "." {
		return DirectoryEntry{File: l}, nil
	}

	if path.Base(name) != name {
		return DirectoryEntry{}, fmt.Errorf("LocalDirectory methods can not handle paths: %s", name)
	}

	childName := filepath.Join(l.filename, name)

	info, err := os.Stat(childName)
	if err != nil {
		return DirectoryEntry{}, err
	}

	if info.IsDir() {
		return DirectoryEntry{File: NewLocalDirectory(childName), Name: name}, nil
	} else {
		return DirectoryEntry{File: NewLocalFile(childName, nil), Name: name}, nil
	}
}

// Readdir implements Directory.
func (l *LocalDirectory) Readdir() ([]DirectoryEntry, error) {
	ents, err := os.ReadDir(l.filename)
	if err != nil {
		return nil, err
	}

	var ret []DirectoryEntry

	for _, ent := range ents {
		var f File

		if ent.IsDir() {
			f = NewLocalDirectory(filepath.Join(l.filename, ent.Name()))
		} else {
			f = NewLocalFile(filepath.Join(l.filename, ent.Name()), nil)
		}

		ret = append(ret, DirectoryEntry{File: f, Name: ent.Name()})
	}

	return ret, nil
}

var (
	_ Directory = &LocalDirectory{}
)

func NewLocalDirectory(filename string) *LocalDirectory {
	return &LocalDirectory{LocalFile: NewLocalFile(filename, nil).(*LocalFile)}
}

type LocalMutableFile struct {
	*LocalFile
}

// Chmod implements MutableFile.
func (l *LocalMutableFile) Chmod(mode fs.FileMode) error {
	return os.Chmod(l.filename, mode)
}

// Chown implements MutableFile.
func (l *LocalMutableFile) Chown(uid int, gid int) error {
	return os.Chown(l.filename, uid, gid)
}

// Chtimes implements MutableFile.
func (l *LocalMutableFile) Chtimes(mtime time.Time) error {
	return os.Chtimes(l.filename, mtime, mtime)
}

// Overwrite implements MutableFile.
func (l *LocalMutableFile) Overwrite(contents []byte) error {
	return os.WriteFile(l.filename, contents, 0644)
}

// Open implements File.
// This shadows the Open method of LocalFile.
func (l *LocalMutableFile) Open() (FileHandle, error) {
	return os.OpenFile(l.filename, os.O_RDWR, 0)
}

var (
	_ MutableFile = &LocalMutableFile{}
)

func NewLocalMutableFile(filename string, source hash.SerializableValue) *LocalMutableFile {
	return &LocalMutableFile{LocalFile: NewLocalFile(filename, source).(*LocalFile)}
}

type LocalMutableDirectory struct {
	*LocalMutableFile
}

// Create implements MutableDirectory.
func (l *LocalMutableDirectory) Create(name string, f File) (File, error) {
	// if the file is a symlink then extract the target and create a symlink.
	if f != nil {
		newInfo, err := f.Stat()
		if err != nil {
			return nil, err
		}

		if newInfo.Kind() == TypeSymlink {
			link, err := GetLinkName(f)
			if err != nil {
				return nil, err
			}

			if err := os.Symlink(link, filepath.Join(l.filename, name)); err != nil {
				return nil, err
			}

			return NewLocalMutableFile(filepath.Join(l.filename, name), nil), nil
		} else if newInfo.Kind() == TypeLink {
			return nil, fmt.Errorf("cannot create a hard link")
		} else if newInfo.Kind() != TypeRegular {
			return nil, fmt.Errorf("cannot create a file of type %s", newInfo.Kind())
		}
	}

	if err := os.WriteFile(filepath.Join(l.filename, name), nil, 0644); err != nil {
		return nil, err
	}

	if f != nil {
		// copy the existing content from the passed file.
		src, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer src.Close()

		dst, err := os.OpenFile(filepath.Join(l.filename, name), os.O_WRONLY, 0)
		if err != nil {
			return nil, err
		}

		if _, err := io.Copy(dst, src); err != nil {
			return nil, err
		}

		if err := dst.Close(); err != nil {
			return nil, err
		}

		newInfo, err := f.Stat()
		if err != nil {
			return nil, err
		}

		// set the mode of the new file to the mode of the passed file.
		if err := os.Chmod(filepath.Join(l.filename, name), newInfo.Mode()); err != nil {
			return nil, err
		}

		// set the mtime of the new file to the mtime of the passed file.
		if err := os.Chtimes(filepath.Join(l.filename, name), newInfo.ModTime(), newInfo.ModTime()); err != nil {
			return nil, err
		}
	}

	return NewLocalMutableFile(filepath.Join(l.filename, name), nil), nil
}

// GetChild implements MutableDirectory.
func (l *LocalMutableDirectory) GetChild(name string) (DirectoryEntry, error) {
	if name == "" || name == "." {
		return DirectoryEntry{File: l}, nil
	}

	if path.Base(name) != name {
		return DirectoryEntry{}, fmt.Errorf("LocalMutableDirectory methods can not handle paths: %s", name)
	}

	childName := filepath.Join(l.filename, name)

	info, err := os.Stat(childName)
	if err != nil {
		return DirectoryEntry{}, err
	}

	if info.IsDir() {
		return DirectoryEntry{File: NewLocalMutableDirectory(childName), Name: name}, nil
	} else {
		return DirectoryEntry{File: NewLocalMutableFile(childName, nil), Name: name}, nil
	}
}

// Mkdir implements MutableDirectory.
func (l *LocalMutableDirectory) Mkdir(name string) (MutableDirectory, error) {
	if err := os.Mkdir(filepath.Join(l.filename, name), 0755); err != nil {
		if os.IsExist(err) {
			return NewLocalMutableDirectory(filepath.Join(l.filename, name)), err
		} else {
			return nil, err
		}
	}

	return NewLocalMutableDirectory(filepath.Join(l.filename, name)), nil
}

// Open implements MutableDirectory.
// Subtle: this method shadows the method (*LocalMutableFile).Open of LocalMutableDirectory.LocalMutableFile.
func (l *LocalMutableDirectory) Open() (FileHandle, error) {
	return nil, fs.ErrInvalid
}

// Overwrite implements MutableDirectory.
// Subtle: this method shadows the method (*LocalMutableFile).Overwrite of LocalMutableDirectory.LocalMutableFile.
func (l *LocalMutableDirectory) Overwrite(contents []byte) error {
	return fs.ErrInvalid
}

// Readdir implements MutableDirectory.
func (l *LocalMutableDirectory) Readdir() ([]DirectoryEntry, error) {
	ents, err := os.ReadDir(l.filename)
	if err != nil {
		return nil, err
	}

	var ret []DirectoryEntry

	for _, ent := range ents {
		var f File

		if ent.IsDir() {
			f = NewLocalMutableDirectory(filepath.Join(l.filename, ent.Name()))
		} else {
			f = NewLocalMutableFile(filepath.Join(l.filename, ent.Name()), nil)
		}

		ret = append(ret, DirectoryEntry{File: f, Name: ent.Name()})
	}

	return ret, nil
}

// Unlink implements MutableDirectory.
func (l *LocalMutableDirectory) Unlink(name string) error {
	return os.RemoveAll(filepath.Join(l.filename, name))
}

var (
	_ MutableDirectory = &LocalMutableDirectory{}
)

func NewLocalMutableDirectory(filename string) *LocalMutableDirectory {
	return &LocalMutableDirectory{LocalMutableFile: NewLocalMutableFile(filename, nil)}
}
