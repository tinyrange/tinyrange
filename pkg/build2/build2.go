package build2

import (
	"io"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

// Glossary:
// - BuildDefinition: A definition of a build that can be executed by a Builder.
// - BuildReceipt: A receipt that describes the build process that was used to create a BuildArtifact.
// - BuildArtifact: An artifact that was created by a Builder.
// - Builder: An interface that can build a BuildDefinition.
// - BuildOutputWriter: An interface that can write the output of a build.
// - BuildContext: A context in which a build can be executed.

type DependencyInfo struct {
	// Hash is the SHA-256 hash of the dependency.
	Hash hash.Hash `json:"hash"`
	// UsedCache is true if the dependency was loaded from the cache.
	UsedCache bool `json:"used_cache"`
	// Explicit is true if the dependency was explicitly added.
	Explicit bool `json:"explicit"`
}

type BuildReceipt struct {
	// Version is the version of TinyRange that built the artifact.
	Version string `json:"version"`
	// Redistributable is true if the artifact can be redistributed.
	Redistributable bool `json:"redistributable"`
	// Non-zero if the build should unconditionally be rebuilt after this time.
	ExpireTime time.Time `json:"expire_time"`
	// Dependencies is a list of dependencies that were used to build the artifact.
	Dependencies []*DependencyInfo `json:"dependencies"`
	// BuiltFor is the parent definition that the artifact was built for.
	BuiltFor hash.Hash `json:"built_for"`
	// OutputHashes is a map of output names to their SHA-256 hashes.
	OutputHashes map[string]string `json:"output_hashes"`
	// BuildTime is the time the artifact was built.
	BuildTime time.Time `json:"build_time"`
	// BuildDuration is the duration of the build.
	BuildDuration time.Duration `json:"build_duration"`
}

type BuildOptions struct {
	// ForceRebuild is true if the build should be forced to rebuild.
	ForceRebuild bool

	// DependencyInfo is the dependency info for the build.
	DependencyInfo *DependencyInfo

	// BlockerFor is the build context that is being blocked by this build.
	BlockerFor BuildContext
}

type Builder interface {
	// Build builds a BuildDefinition returning a build artifact.
	Build(def BuildDefinition, options BuildOptions) (BuildArtifact, error)

	// BuildChild builds a BuildDefinition as a child of a parent BuildContext.
	BuildChild(parent BuildContext, def BuildDefinition, options BuildOptions) (BuildArtifact, error)

	// HashDefinition hashes a BuildDefinition.
	HashDefinition(def BuildDefinition) (hash.Hash, error)

	// SerializeDefinition serializes a BuildDefinition.
	MarshalDefinition(def BuildDefinition) ([]byte, error)

	// ArtifactFromHash gets a BuildArtifact from a hash.
	ArtifactFromHash(hash hash.Hash) (BuildArtifact, error)

	// DefinitionFromArtifact gets a BuildDefinition from a BuildArtifact.
	DefinitionFromArtifact(artifact BuildArtifact) (BuildDefinition, error)

	// GarbageCollect returns a list of garbage collectable hashes.
	GarbageCollect(olderThan time.Time) ([]hash.Hash, error)
}

func NewBuilder(buildDirectory filesystem.MutableDirectory, maxParallelism int, logger BuildLogger) Builder {
	b := &builder{
		buildDirectory: buildDirectory,
		buildCache:     make(map[hash.Hash]*buildInfo),
		tl:             newTokenLocker(maxParallelism),
		logger:         logger,
	}
	b.hashDb = hash.NewDefinitionDatabase(b.hashCacheMiss)
	return b
}

type BuildArtifact interface {
	// Hash returns the SHA-256 hash of the artifact.
	Hash() hash.Hash

	// Receipt returns the BuildReceipt for the artifact.
	Receipt() BuildReceipt

	// RawDefinition returns the raw definition for the artifact.
	RawDefinition() (io.ReadCloser, error)

	// GetOutput returns an output file by name.
	GetOutput(name string) (filesystem.File, error)

	// ListOutputs returns a list of output names.
	ListOutputs() ([]string, error)
}

type BuildOutputWriter interface {
	io.WriteCloser
}

type BuildContext interface {
	// Hash returns the SHA-256 hash of the build context.
	Hash() hash.Hash

	// BuildChild builds a child BuildDefinition.
	BuildChild(def BuildDefinition, opts BuildOptions) (BuildArtifact, error)

	// CreateOutput creates a new output file.
	CreateOutput(name string) (BuildOutputWriter, error)

	// SetRedistributable sets the redistributable flag.
	// If the redistributable flag is true, the build artifact can be redistributed automatically.
	SetRedistributable(redistributable bool)

	// SetExpireTime sets the expire time.
	// If the expire time is non-zero, the build will be unconditionally rebuilt after this time.
	SetExpireTime(expireTime time.Time)

	// Logf logs a message.
	Logf(format string, args ...any)

	// LogGroup returns the log group.
	LogGroup() Group
}

type BuildDefinition interface {
	hash.Definition

	// Build builds the definition.
	Build(ctx BuildContext) error

	// Dependencies returns a list of explicit dependencies.
	// Explicit dependencies are automatically built as the definition starts building.
	Dependencies() ([]BuildDefinition, error)
}

type EventSink interface {
	SendEvent(event)
}

type Group interface {
	io.Closer

	Logf(string, ...interface{})
	Subgroup(name string) Group
	Description(string, ...interface{})
}

type BuildLogger interface {
	Run(io.Writer) error
	Group(name string) Group
	Close() error
}
