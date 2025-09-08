package common

import (
	"io"
	"net/http"

	protob "google.golang.org/protobuf/proto"

	"github.com/tinyrange/tinyrange/archive"
	"github.com/tinyrange/tinyrange/build/hash"
	"github.com/tinyrange/tinyrange/build/proto"
)

const (
	TYPE_NAME_EXTRACT_ARCHIVE = "tinyrange/alpha/extract_archive"
	TYPE_NAME_FETCH_HTTP      = "tinyrange/alpha/fetch_http"
)

type FileType string

const (
	FileType_Plain           FileType = "text/plain"
	FileType_ArchiveIndex    FileType = "application/x-tinyrange-archive-index"
	FileType_ArchiveContents FileType = "application/x-tinyrange-archive-contents"
)

type WritableFile interface {
	io.WriteCloser
}

type ArchiveWriter interface {
	io.Closer
	WriteEntry(entry *archive.EntryFactory, r io.Reader) error
}

type Context interface {
	Hash() hash.Hash
	Decode(msg protob.Message) error

	HttpClient() *http.Client

	Create(ft FileType) (WritableFile, error)
	CreateArchive() (ArchiveWriter, error)

	ProgressBar(name string, size int64, r io.ReadCloser) io.ReadCloser
}

type BuildClosure = *proto.BuildClosure

type Artifact interface {
}

type Option interface {
	apply()
}

type Builder interface {
	Build(ctx Context) error
}

type Database interface {
	Factory() Factory
	Build(def BuildClosure, opt ...Option) (Artifact, error)
}

type Factory interface {
	NewFetchHttp(url string) BuildClosure
	NewExtractArchive(src BuildClosure, archiveType proto.ArchiveType, compressionType proto.CompressionType) BuildClosure
}
