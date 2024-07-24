package common

import (
	"io"
	"time"

	"go.starlark.net/starlark"
)

type BuildResult interface {
	io.WriterTo
}

// Marshallable objects can be safely serialized as part of a parameter set.
// They can only be serialized if they are not pointers.
type MarshallableObject interface {
	TagMarshallableObject()
}

type BuildDefinitionParameters interface {
	TagParameters()
}

type BuildDefinition interface {
	// A unique type name for this build definition.
	Type() string

	// Create a new instance of this build definition with a given set of parameters.
	Create(params BuildDefinitionParameters) BuildDefinition

	// Return the set of parameters associated with this definition.
	Params() BuildDefinitionParameters

	// Returns true if the definition needs to be rebuilt.
	// The cacheTime is the last time this definition was successfully rebuilt.
	NeedsBuild(ctx BuildContext, cacheTime time.Time) (bool, error)

	// Build the definition. Returns some kind of build result which will be written to the output file.
	Build(ctx BuildContext) (BuildResult, error)
}

type BuildContext interface {
	Database() PackageDatabase

	CreateFile(name string) (string, io.WriteCloser, error)

	CreateOutput() (io.WriteCloser, error)
	HasCreatedOutput() bool

	SetHasCached()
	HasCached() bool

	SetInMemory()
	IsInMemory() bool

	BuildChild(def BuildDefinition) (File, error)
	NeedsBuild(def BuildDefinition) (bool, error)

	Call(callable starlark.Callable, args ...starlark.Value) (starlark.Value, error)

	ChildContext(buildDef BuildDefinition, status *BuildStatus, filename string) BuildContext

	FileFromDigest(digest *FileDigest) (File, error)
	FilenameFromDigest(digest *FileDigest) (string, error)
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
	Children []BuildDefinition
}
