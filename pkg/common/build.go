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
