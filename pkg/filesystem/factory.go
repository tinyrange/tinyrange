package filesystem

import (
	"io/fs"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/hash"
)

type fileFactory struct {
}

// NewMemoryFile implements FileFactory.
func (f *fileFactory) NewMemoryFile() MutableFile {
	return &memoryFile{
		fac:   f,
		kind:  TypeRegular,
		mode:  fs.FileMode(0755),
		mTime: time.Now(),
	}
}

// NewSymlink implements FileFactory.
func (f *fileFactory) NewSymlink(target string) MutableFile {
	return &memoryFile{
		fac:      f,
		kind:     TypeSymlink,
		mode:     fs.ModeSymlink | fs.FileMode(0755),
		contents: []byte(target),
	}
}

// NewHardLink implements FileFactory.
func (f *fileFactory) NewHardLink(target string) (MutableFile, error) {
	target = strings.TrimPrefix(target, ".")
	if !strings.HasPrefix(target, "/") {
		target = "/" + target
	}

	return &memoryFile{
		fac:      f,
		kind:     TypeLink,
		mode:     fs.FileMode(0755),
		contents: []byte(target),
	}, nil
}

// NewMemoryDirectory implements FileFactory.
func (f *fileFactory) NewMemoryDirectory() MutableDirectory {
	return &memoryDirectory{
		memoryFile: &memoryFile{
			fac:   f,
			kind:  TypeDirectory,
			mode:  fs.ModeDir | fs.FileMode(0755),
			mTime: time.Now(),
		},
		entries: make(map[string]File),
	}
}

// NewLocalFile implements FileFactory.
func (f *fileFactory) NewLocalFile(filename string, source hash.SerializableValue) File {
	return &localFile{
		fac:      f,
		filename: filename,
		source:   source,
	}
}

// NewLocalDirectory implements FileFactory.
func (f *fileFactory) NewLocalDirectory(filename string) Directory {
	return &localDirectory{
		localFile: f.NewLocalFile(filename, nil).(*localFile),
	}
}

// NewLocalMutableFile implements FileFactory.
func (f *fileFactory) NewLocalMutableFile(filename string, source hash.SerializableValue) MutableFile {
	return &localMutableFile{
		localFile: f.NewLocalFile(filename, source).(*localFile),
	}
}

// NewLocalMutableDirectory implements FileFactory.
func (f *fileFactory) NewLocalMutableDirectory(filename string) MutableDirectory {
	return &localMutableDirectory{
		localMutableFile: f.NewLocalMutableFile(filename, nil).(*localMutableFile),
	}
}

var (
	_ FileFactory = &fileFactory{}
)

var Factory = &fileFactory{}
