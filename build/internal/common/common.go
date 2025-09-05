package common

import (
	protob "google.golang.org/protobuf/proto"

	"github.com/tinyrange/tinyrange/build/hash"
	"github.com/tinyrange/tinyrange/build/proto"
)

const (
	TYPE_NAME_EXTRACT_ARCHIVE = "tinyrange/alpha/extract_archive"
	TYPE_NAME_FETCH_HTTP      = "tinyrange/alpha/fetch_http"
)

type Context interface {
	Hash() hash.Hash
	Decode(msg protob.Message) error
}

type Definition = *proto.Definition

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
	Build(def Definition, opt ...Option) (Artifact, error)
}

type Factory interface {
	NewFetchHttp(url string) Definition
	NewExtractArchive(src Definition, archiveType proto.ArchiveType, compressionType proto.CompressionType) Definition
}
