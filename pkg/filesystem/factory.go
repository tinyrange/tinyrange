package filesystem

import (
	"encoding/json"
	"io/fs"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/hash"
)

type fileFactory struct {
	valuesEncoder *json.Encoder
}

func (f *fileFactory) LogEvent(event string, args ...any) {
	if f.valuesEncoder == nil {
		return
	}

	values := map[string]any{
		"time":  time.Now().Format(time.RFC3339),
		"event": event,
	}

	// args is formatted as a series of key=value pairs.

	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) {
			break
		}

		key, ok := args[i].(string)
		if !ok {
			continue
		}

		values[key] = args[i+1]
	}

	f.valuesEncoder.Encode(values)
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

func SetFileAccessLogger(encoder *json.Encoder) {
	if encoder == nil {
		return
	}

	// Set the encoder to the factory.
	Factory.valuesEncoder = encoder
}

var Factory = &fileFactory{}
