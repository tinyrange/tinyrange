package build2

import (
	"fmt"
	"io"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

type BuildOptions struct {
	ForceRebuild bool
}

type BuildReceipt struct {
	Requirements []hash.Hash       `json:"requirements"`
	StartTime    time.Time         `json:"start_time"`
	Duration     time.Duration     `json:"duration"`
	Files        map[string]string `json:"files"` // map of filename to sha256 hash
}

// BuildResult is implemented by definitions and called with a writer.
type BuildResult interface {
	// WriteResult writes the result of the build to the given writer.
	WriteResult(out io.Writer) error
}

// BuildArtifact is the result of a build.
type BuildArtifact interface {
	// Hash returns the hash of the definition.
	Hash() hash.Hash
	// Receipt returns the receipt of the build.
	Receipt() BuildReceipt
	// Default returns the default file written with WriteDefault.
	Default() (filesystem.File, error)
	// OpenFile opens a file in the artifact.
	OpenFile(name string) (filesystem.FileHandle, error)
}

// BuildContext is the context of a build.
type BuildContext interface {
	// BuildChild builds a child definition.
	BuildChild(def BuildDefinition) (BuildArtifact, error)
	// CreateFile creates a file in the build context.
	CreateFile(name string) (io.WriteCloser, error)
	// WriteDefault writes the default file for the build.
	WriteDefault(result BuildResult) error
	// Hash returns the hash of the build context.
	Hash() hash.Hash
	// LastBuild returns the time of the last build.
	LastBuild() time.Time
	// Describe logs a message about the build.
	Describe(format string, args ...interface{})
	// Logf logs a message about the build.
	Logf(format string, args ...interface{})
}

// BuildDefinition is a definition that can be built and cached.
type BuildDefinition interface {
	hash.Definition
	fmt.Stringer

	// NeedsBuild returns whether the definition needs to be rebuilt.
	NeedsBuild(ctx BuildContext) (bool, error)
	// Dependencies returns the dependencies of the definition.
	Dependencies() ([]BuildDefinition, error)
	// Build builds the definition and returns the result.
	Build(ctx BuildContext) error
}

// Builder is the root object used to build definitions.
type Builder interface {
	// Build builds a definition.
	Build(def BuildDefinition, opts BuildOptions) (BuildArtifact, error)
	// GarbageCollect removes old build artifacts.
	GarbageCollect(olderThan time.Time) ([]hash.Hash, error)
}
