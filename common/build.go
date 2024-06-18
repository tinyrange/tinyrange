package common

import (
	"io"
	"time"

	"github.com/tinyrange/pkg2/v2/filesystem"
	"go.starlark.net/starlark"
)

type BuildResult interface {
	io.WriterTo
}

type BuildDefinition interface {
	BuildSource
	NeedsBuild(ctx BuildContext, cacheTime time.Time) (bool, error)
	Build(ctx BuildContext) (BuildResult, error)
}

type BuildContext interface {
	HasCreatedOutput() bool
	SetInMemory()
	IsInMemory() bool
	Database() PackageDatabase
	BuildChild(def BuildDefinition) (filesystem.File, error)
	NeedsBuild(def BuildDefinition) (bool, error)
	Call(callable starlark.Callable, args ...starlark.Value) (starlark.Value, error)
	ChildContext(source BuildSource, status *BuildStatus, filename string) BuildContext
	Packages() []*Package
	FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error)
}

type BuildSource interface {
	Tag() string
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
