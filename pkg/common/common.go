package common

import (
	"encoding/gob"
	"io"
	"io/fs"

	"go.starlark.net/starlark"
)

type FileSource interface{}

type ExtractArchiveSource struct {
	Kind            string // "ExtractArchive"
	Source          FileSource
	Extension       string
	StripComponents int
}

type DecompressSource struct {
	Kind      string // "Decompress"
	Source    FileSource
	Extension string
}

type DownloadSource struct {
	Kind   string // "Download"
	Url    string
	Accept string
}

type LocalFileSource struct {
	Kind     string // "LocalFile"
	Filename string
}

func init() {
	gob.Register(ExtractArchiveSource{})
	gob.Register(DecompressSource{})
	gob.Register(DownloadSource{})
	gob.Register(LocalFileSource{})
}

type FileInfo interface {
	fs.FileInfo
	OwnerGroup() (int, int)
	Linkname() string
}

type FileIf interface {
	Open() (io.ReadCloser, error)
	Stat() (FileInfo, error)
}

type StarFileIf interface {
	starlark.Value
	FileIf
	Source() FileSource
	Name() string
	SetName(name string) (StarFileIf, error)
}

type StarDirectory interface {
	StarFileIf

	OpenChild(path string, mkdir bool) (StarFileIf, bool, error)

	Entries() map[string]StarFileIf
}
