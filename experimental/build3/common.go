package main

import (
	"fmt"
	"io"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

const (
	definitionFileName = "definition.json"
	receiptFileName    = "receipt.json"
	outputPrefix       = "output."
)

type Color int

const (
	ColorDefault Color = iota
	ColorRed
	ColorGreen
	ColorYellow
	ColorBlue
	ColorGrey
)

type Logger interface {
	io.Closer

	Logf(format string, args ...interface{})
	Describe(color Color, format string, args ...interface{})
	Child(description string) Logger
}

type BuildOptions struct {
	ForceRebuild bool
}

type BuildReceipt struct {
	Requirements []hash.Hash       `json:"requirements"`
	StartTime    time.Time         `json:"start_time"`
	Duration     time.Duration     `json:"duration"`
	Files        map[string]string `json:"files"` // map of filename to sha256 hash
}

type BuildArtifact interface {
	Hash() hash.Hash
	Receipt() BuildReceipt

	OpenFile(name string) (filesystem.FileHandle, error)
}

type BuildContext interface {
	BuildChild(def BuildDefinition) (BuildArtifact, error)

	Describe(format string, args ...interface{})
	Logf(format string, args ...interface{})

	CreateFile(name string) (io.WriteCloser, error)

	Hash() hash.Hash
	LastBuild() time.Time
}

type BuildDefinition interface {
	hash.Definition
	fmt.Stringer

	NeedsBuild(ctx BuildContext) (bool, error)
	Dependencies() ([]BuildDefinition, error)
	Build(ctx BuildContext) error
}

type Builder interface {
	Build(def BuildDefinition, opts BuildOptions) (BuildArtifact, error)
	GarbageCollect(olderThan time.Time) ([]hash.Hash, error)
}
