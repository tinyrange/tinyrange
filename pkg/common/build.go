package common

import (
	"io"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

type BuildResult interface {
	WriteResult(out io.Writer) error
}

type BuildSource interface {
	Tag() string
}

type DependencyNode interface {
	hash.SerializableValue
	Dependencies(ctx BuildContext) ([]DependencyNode, error)
}

type BuildDefinition interface {
	hash.Definition
	BuildSource
	DependencyNode
	MacroResult
	NeedsBuild(ctx BuildContext, cacheTime time.Time) (bool, error)
	Build(ctx BuildContext) (BuildResult, error)
	ToStarlark(ctx BuildContext, result filesystem.File) (starlark.Value, error)
}

type RedistributableDefinition interface {
	BuildDefinition
	Redistributable() bool
}

type BuildContext interface {
	starlark.Value

	DisplayTree()
	CreateOutput() (io.WriteCloser, error)
	CreateFile(name string) (string, io.WriteCloser, error)
	HasCreatedOutput() bool
	SetHasCached()
	HasCached() bool
	Database() PackageDatabase
	BuildChild(def BuildDefinition) (filesystem.File, error)
	NeedsBuild(def BuildDefinition) (bool, error)
	Call(filename string, builder string, args ...starlark.Value) (starlark.Value, error)
	ChildContext(source BuildSource, status *BuildStatus, filename string) BuildContext
	FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error)
	FilenameFromDigest(digest *filesystem.FileDigest) (string, error)
}

type MacroResult interface {
}

type BuildStatusKind byte

const (
	BuildStatusBuilt BuildStatusKind = iota
	BuildStatusCached
)

func (s BuildStatusKind) String() string {
	switch s {
	case BuildStatusBuilt:
		return "Built"
	case BuildStatusCached:
		return "Cached"
	default:
		return "<unknown BuildStatusKind>"
	}
}

type BuildStatus struct {
	Status   BuildStatusKind
	Tag      string
	Children []BuildDefinition
}
