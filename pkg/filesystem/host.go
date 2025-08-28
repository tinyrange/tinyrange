package filesystem

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"time"

	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/path"
)

var ErrNotSupported = errors.New("operation not supported on this platform")

type osStat struct {
	fs.FileInfo
}

// Id implements FileInfo.
func (o *osStat) Id() uint64 {
	return hashId(uniqueIdFromFileInfo(o.FileInfo))
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

type readOnlyFileHandle struct {
	fh *os.File
}

// Close implements FileHandle.
func (r *readOnlyFileHandle) Close() error {
	return r.fh.Close()
}

// Read implements FileHandle.
func (r *readOnlyFileHandle) Read(p []byte) (n int, err error) {
	return r.fh.Read(p)
}

// ReadAt implements FileHandle.
func (r *readOnlyFileHandle) ReadAt(p []byte, off int64) (n int, err error) {
	return r.fh.ReadAt(p, off)
}

var (
	_ FileHandle = &readOnlyFileHandle{}
)

type localFile struct {
	fac      FileFactory
	filename string
	source   hash.SerializableValue
}

func (l *localFile) log(action string) {
	l.fac.LogEvent("localFile",
		"filename", l.filename,
		"action", action,
	)
}

// Open implements File.
func (l *localFile) Open() (FileHandle, error) {
	l.log("open")

	fh, err := os.OpenFile(l.filename, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}

	return &readOnlyFileHandle{fh: fh}, nil
}

// Stat implements File.
func (l *localFile) Stat() (FileInfo, error) {
	l.log("stat")

	s, err := os.Stat(l.filename)
	if err != nil {
		return nil, err
	}

	return &osStat{FileInfo: s}, nil
}

// Filename implements HostFile.
func (l *localFile) Filename() (string, error) {
	return l.filename, nil
}

// Lstat implements Symlink.
func (l *localFile) Lstat() (FileInfo, error) {
	l.log("lstat")

	s, err := os.Lstat(l.filename)
	if err != nil {
		return nil, err
	}

	return &osStat{FileInfo: s}, nil
}

// Readlink implements Symlink.
func (l *localFile) Readlink() (string, error) {
	l.log("readlink")

	return os.Readlink(l.filename)
}

var (
	_ File     = &localFile{}
	_ Symlink  = &localFile{}
	_ HostFile = &localFile{}
)

type localDirectory struct {
	*localFile
}

func (l *localDirectory) log(action string, args ...interface{}) {
	l.fac.LogEvent("localDirectory",
		append(
			[]any{
				"filename", l.filename,
				"action", action,
			},
			args...,
		)...,
	)
}

// GetChild implements Directory.
func (l *localDirectory) GetChild(name string) (DirectoryEntry, error) {
	if name == "" || name == "." {
		return DirectoryEntry{File: l}, nil
	}

	if path.Unix.Base(name) != name {
		return DirectoryEntry{}, fmt.Errorf("LocalDirectory methods can not handle paths: %s", name)
	}

	childName := path.Native.Join(l.filename, name)

	info, err := os.Stat(childName)
	if err != nil {
		return DirectoryEntry{}, err
	}

	if info.IsDir() {
		l.log("get-child", "directory", true, "child-name", childName)

		return DirectoryEntry{File: l.fac.NewLocalDirectory(childName), Name: name}, nil
	} else {
		l.log("get-child", "directory", false, "child-name", childName)

		return DirectoryEntry{File: l.fac.NewLocalFile(childName, nil), Name: name}, nil
	}
}

// Readdir implements Directory.
func (l *localDirectory) Readdir() ([]DirectoryEntry, error) {
	l.log("readdir")

	ents, err := os.ReadDir(l.filename)
	if err != nil {
		return nil, err
	}

	var ret []DirectoryEntry

	for _, ent := range ents {
		var f File

		if ent.IsDir() {
			f = l.fac.NewLocalDirectory(path.Native.Join(l.filename, ent.Name()))
		} else {
			f = l.fac.NewLocalFile(path.Native.Join(l.filename, ent.Name()), nil)
		}

		ret = append(ret, DirectoryEntry{File: f, Name: ent.Name()})
	}

	return ret, nil
}

var (
	_ Directory = &localDirectory{}
)

type localMutableFile struct {
	*localFile
}

func (l *localMutableFile) log(action string, args ...interface{}) {
	l.fac.LogEvent("localMutableFile",
		append(
			[]any{
				"filename", l.filename,
				"action", action,
			},
			args...,
		)...,
	)
}

// Chmod implements MutableFile.
func (l *localMutableFile) Chmod(mode fs.FileMode) error {
	return os.Chmod(l.filename, mode)
}

// Chown implements MutableFile.
func (l *localMutableFile) Chown(uid int, gid int) error {
	if runtime.GOOS == "windows" {
		return ErrNotSupported
	}

	l.log("chown", "uid", uid, "gid", gid)

	return os.Chown(l.filename, uid, gid)
}

// Chtimes implements MutableFile.
func (l *localMutableFile) Chtimes(mtime time.Time) error {
	l.log("chtimes", "mtime", mtime)

	return os.Chtimes(l.filename, mtime, mtime)
}

// Overwrite implements MutableFile.
func (l *localMutableFile) Overwrite(contents []byte) error {
	l.log("overwrite", "contents", contents)

	return os.WriteFile(l.filename, contents, 0644)
}

// Truncate implements MutableFile.
func (l *localMutableFile) Truncate(size int64) error {
	l.log("truncate", "size", size)

	return os.Truncate(l.filename, size)
}

// Open implements File.
// This shadows the Open method of LocalFile.
func (l *localMutableFile) Open() (FileHandle, error) {
	l.log("open")

	fh, err := os.OpenFile(l.filename, os.O_RDWR, 0)
	if errors.Is(err, os.ErrPermission) {
		l.log("open", "read-only", true)

		// try to open the file in read-only mode.
		fh, err = os.OpenFile(l.filename, os.O_RDONLY, 0)
		if err != nil {
			return nil, err
		}

		// if the file is read-only, then return a read-only file handle.
		return &readOnlyFileHandle{fh: fh}, nil
	} else if err != nil {
		return nil, err
	}

	return fh, nil
}

// OpenMut implements MutableFile.
func (l *localMutableFile) OpenMut() (WritableFileHandle, error) {
	l.log("open-mut")

	return os.OpenFile(l.filename, os.O_RDWR, 0)
}

// OpenMutAppend implements MutableAppendFile.
func (l *localMutableFile) OpenMutAppend() (io.WriteCloser, error) {
	l.log("open-mut-append")

	return os.OpenFile(l.filename, os.O_APPEND|os.O_WRONLY, 0)
}

// Rename implements MutableRenameFile.
func (l *localMutableFile) Rename(newDirectory MutableDirectory, newName string) error {
	if mut, ok := newDirectory.(*localMutableDirectory); ok {
		l.log("rename", "new-directory", mut.filename, "new-name", newName)

		return os.Rename(l.filename, path.Native.Join(mut.filename, newName))
	}

	return fs.ErrInvalid
}

var (
	_ MutableFile       = &localMutableFile{}
	_ MutableAppendFile = &localMutableFile{}
	_ MutableRenameFile = &localMutableFile{}
)

type localMutableDirectory struct {
	*localMutableFile
}

func (l *localMutableDirectory) log(action string, args ...interface{}) {
	l.fac.LogEvent("localMutableDirectory",
		append(
			[]any{
				"filename", l.filename,
				"action", action,
			},
			args...,
		)...,
	)
}

// Create implements MutableDirectory.
func (l *localMutableDirectory) Create(name string, f File) (File, error) {
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

			l.log("create", "symlink", "true", "link", link)

			if err := os.Symlink(link, path.Native.Join(l.filename, name)); err != nil {
				return nil, err
			}

			return l.fac.NewLocalMutableFile(path.Native.Join(l.filename, name), nil), nil
		} else if newInfo.Kind() == TypeLink {
			return nil, fmt.Errorf("cannot create a hard link")
		} else if newInfo.Kind() != TypeRegular {
			return nil, fmt.Errorf("cannot create a file of type %s", newInfo.Kind())
		}
	}

	l.log("create", "name", name)

	if err := os.WriteFile(path.Native.Join(l.filename, name), nil, 0644); err != nil {
		return nil, err
	}

	if f != nil {
		// copy the existing content from the passed file.
		src, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer src.Close()

		dst, err := os.OpenFile(path.Native.Join(l.filename, name), os.O_WRONLY, 0)
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
		if err := os.Chmod(path.Native.Join(l.filename, name), newInfo.Mode()); err != nil {
			return nil, err
		}

		// set the mtime of the new file to the mtime of the passed file.
		if err := os.Chtimes(path.Native.Join(l.filename, name), newInfo.ModTime(), newInfo.ModTime()); err != nil {
			return nil, err
		}
	}

	return l.fac.NewLocalMutableFile(path.Native.Join(l.filename, name), nil), nil
}

// GetChild implements MutableDirectory.
func (l *localMutableDirectory) GetChild(name string) (DirectoryEntry, error) {
	if name == "" || name == "." {
		return DirectoryEntry{File: l}, nil
	}

	if path.Unix.Base(name) != name {
		return DirectoryEntry{}, fmt.Errorf("LocalMutableDirectory methods can not handle paths: %s", name)
	}

	childName := path.Native.Join(l.filename, name)

	info, err := os.Stat(childName)
	if err != nil {
		return DirectoryEntry{}, err
	}

	if info.IsDir() {
		l.log("get-child", "directory", true, "child-name", childName)

		return DirectoryEntry{File: l.fac.NewLocalMutableDirectory(childName), Name: name}, nil
	} else {
		l.log("get-child", "directory", false, "child-name", childName)

		return DirectoryEntry{File: l.fac.NewLocalMutableFile(childName, nil), Name: name}, nil
	}
}

// Mkdir implements MutableDirectory.
func (l *localMutableDirectory) Mkdir(name string) (MutableDirectory, error) {
	if err := os.Mkdir(path.Native.Join(l.filename, name), 0755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			// fallthrough to return the existing directory.
		} else {
			return nil, err
		}
	}

	l.log("mkdir", "child-name", path.Native.Join(l.filename, name))

	return l.fac.NewLocalMutableDirectory(path.Native.Join(l.filename, name)), nil
}

// Open implements MutableDirectory.
// Subtle: this method shadows the method (*LocalMutableFile).Open of LocalMutableDirectory.LocalMutableFile.
func (l *localMutableDirectory) Open() (FileHandle, error) {
	return nil, fs.ErrInvalid
}

// OpenMut implements MutableDirectory.
func (l *localMutableDirectory) OpenMut() (WritableFileHandle, error) {
	return nil, fs.ErrInvalid
}

// Overwrite implements MutableDirectory.
// Subtle: this method shadows the method (*LocalMutableFile).Overwrite of LocalMutableDirectory.LocalMutableFile.
func (l *localMutableDirectory) Overwrite(contents []byte) error {
	return fs.ErrInvalid
}

// Readdir implements MutableDirectory.
func (l *localMutableDirectory) Readdir() ([]DirectoryEntry, error) {
	l.log("readdir")

	ents, err := os.ReadDir(l.filename)
	if err != nil {
		return nil, err
	}

	var ret []DirectoryEntry

	for _, ent := range ents {
		var f File

		if ent.IsDir() {
			f = l.fac.NewLocalMutableDirectory(path.Native.Join(l.filename, ent.Name()))
		} else {
			f = l.fac.NewLocalMutableFile(path.Native.Join(l.filename, ent.Name()), nil)
		}

		ret = append(ret, DirectoryEntry{File: f, Name: ent.Name()})
	}

	return ret, nil
}

// Unlink implements MutableDirectory.
func (l *localMutableDirectory) Unlink(name string) error {
	l.log("unlink", "name", name)

	return os.RemoveAll(path.Native.Join(l.filename, name))
}

var (
	_ MutableDirectory = &localMutableDirectory{}
)

func GetHostFilename(f File) (string, error) {
	switch f := f.(type) {
	case HostFile:
		return f.Filename()
	case *sourceWrapper:
		return GetHostFilename(f.File)
	default:
		return "", fmt.Errorf("file is not a host file")
	}
}
