package db

import "encoding/gob"

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
