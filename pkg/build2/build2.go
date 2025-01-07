package build2

import (
	"github.com/tinyrange/tinyrange/pkg/build2/internal"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
)

// Glossary:
// - BuildDefinition: A definition of a build that can be executed by a Builder.
// - BuildReceipt: A receipt that describes the build process that was used to create a BuildArtifact.
// - BuildArtifact: An artifact that was created by a Builder.
// - Builder: An interface that can build a BuildDefinition.
// - BuildOutputWriter: An interface that can write the output of a build.
// - BuildContext: A context in which a build can be executed.

// export public symbols
type (
	DependencyInfo    = internal.DependencyInfo
	BuildReceipt      = internal.BuildReceipt
	BuildOptions      = internal.BuildOptions
	Builder           = internal.Builder
	BuildArtifact     = internal.BuildArtifact
	BuildOutputWriter = internal.BuildOutputWriter
	BuildContext      = internal.BuildContext
	BuildDefinition   = internal.BuildDefinition

	// Logging
	LogGroup = internal.LogGroup
	Logger   = internal.Logger
)

func NewBuilder(buildDirectory filesystem.MutableDirectory, maxParallelism int, logger Logger) Builder {
	return internal.NewBuilder(buildDirectory, maxParallelism, logger)
}

func NewBuildLogger(eventBacklog int) Logger {
	return internal.NewBuildLogger(eventBacklog)
}

func NewSimpleLogger() Logger {
	return internal.NewSimpleLogger()
}
