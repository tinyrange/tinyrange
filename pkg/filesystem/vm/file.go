package vm

import (
	"fmt"
	"io"
	"io/fs"
)

type FileRegion struct {
	f         io.ReaderAt
	totalSize int64
}

func (f *FileRegion) String() string {
	return fmt.Sprintf("FileRegion{file=%+v, totalSize=%d}", f.f, f.totalSize)
}

// ReadAt implements MemoryRegion.
func (f *FileRegion) ReadAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(f, off); err != nil {
		return 0, err
	}

	n, err = f.f.ReadAt(p, off)
	if err == io.EOF {
		err = nil
	}
	return
}

// Size implements MemoryRegion.
func (f *FileRegion) Size() int64 {
	return f.totalSize
}

// WriteAt implements MemoryRegion.
func (f *FileRegion) WriteAt(p []byte, off int64) (n int, err error) {
	if err := boundsCheck(f, off); err != nil {
		return 0, err
	}

	return 0, fmt.Errorf("region is read only")
}

var (
	_ MemoryRegion = &FileRegion{}
)

type File interface {
	io.ReaderAt
	Stat() (fs.FileInfo, error)
}

func NewReaderRegion(r io.ReaderAt, size int64) *FileRegion {
	return &FileRegion{f: r, totalSize: size}
}

func NewFileRegion(f File) (*FileRegion, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	return NewReaderRegion(f, info.Size()), nil
}
