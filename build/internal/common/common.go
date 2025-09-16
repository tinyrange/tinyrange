package common

import (
	"io"
	"net/http"

	gproto "google.golang.org/protobuf/proto"

	"github.com/tinyrange/tinyrange/archive"
	"github.com/tinyrange/tinyrange/build/proto"
)

const (
	TYPE_NAME_EXTRACT_ARCHIVE = "tinyrange/alpha/extract_archive"
	TYPE_NAME_FETCH_HTTP      = "tinyrange/alpha/fetch_http"
	TYPE_NAME_WRITE_FILE      = "tinyrange/alpha/write_file"
)

type FileType string

const (
	FileType_Plain           FileType = "text/plain"
	FileType_ArchiveIndex    FileType = "application/x-tinyrange-archive-index"
	FileType_ArchiveContents FileType = "application/x-tinyrange-archive-contents"
)

type File interface {
	io.Reader
	io.ReaderAt
	io.Closer
}

type WritableFile interface {
	io.WriteCloser

	Hash() *proto.Hash
}

type ArchiveWriter interface {
	io.Closer
	WriteEntry(entry *archive.Entry, r io.Reader) error
}

type Context interface {
	Hash() *proto.Hash
	Decode(msg gproto.Message) error

	HttpClient() *http.Client

	Create(ft FileType) (WritableFile, error)
	CreateArchive() (ArchiveWriter, error)

	ProgressBar(name string, size int64, r io.ReadCloser) io.ReadCloser
}

type BuildClosure = *proto.BuildClosure

type Artifact interface {
	Open(ft FileType) (File, error)

	Definition() (*proto.Definition, error)
	Receipt() (*proto.BuildReceipt, error)
}

type Option interface {
	apply(opt *BuildOptions)
}

type optionFunc func(opt *BuildOptions)

func (f optionFunc) apply(opt *BuildOptions) { f(opt) }

type BuildOptions struct {
	status func(*proto.BuildStatus)
}

func WithStatusCallback(f func(*proto.BuildStatus)) Option {
	return optionFunc(func(opt *BuildOptions) {
		opt.status = f
	})
}

type Builder interface {
	Build(ctx Context) error
}

type BuilderMetadata struct {
	Builder    Builder
	TypeName   string
	Definition gproto.Message
}

type BuildCacheDirectory interface {
	ReadDefinition() ([]byte, error)
	ReadReceipt() ([]byte, error)

	OpenFile(ft FileType) (File, error)
}

type WritableBuildCacheDirectory interface {
	BuildCacheDirectory

	WriteDefinition(content []byte) error
	WriteReceipt(content []byte) error

	CreateFile(ft FileType) (WritableFile, error)
}

type BuildCache interface {
	// Returns fs.ErrNotExist if not found.
	OpenRead(h *proto.Hash) (BuildCacheDirectory, error)
	OpenWrite(h *proto.Hash) (WritableBuildCacheDirectory, error)
}

type Database interface {
	Factory() Factory

	Build(def BuildClosure, opt ...Option) (Artifact, error)
	GetBuildStatus(hash string) (proto.CurrentBuildStatus, error)

	GetBuilders() ([]BuilderMetadata, error)
}

type Factory interface {
	NewFetchHttp(url string) BuildClosure
	NewExtractArchive(src BuildClosure, archiveType proto.ArchiveType, compressionType proto.CompressionType) BuildClosure
	NewWriteFile(content []byte) BuildClosure
}
