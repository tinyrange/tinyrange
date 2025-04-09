package common

import (
	"io"
	"time"

	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

type BuildVmDefinition interface {
	StarBuildDefinition
	Directive

	SetBuildTemplateMode()
}

type FetchOciImageDefinition interface {
	StarBuildDefinition
	Directive
}

type ReadOCIImageDefinition interface {
	BuildDefinition
	Directive
}

type BuildFSDefinition interface {
	StarBuildDefinition
	Directive
}

type ReadArchiveDefinition interface {
	StarBuildDefinition
	Directive
}

type PlanDefinition interface {
	BuildDefinition
	InstallationPlanBuilder
	Directive

	AddPackage(name PackageQuery) (PlanDefinition, error)
}

type BuilderFunc func() (io.ReadCloser, error)

type DefinitionFactory interface {
	NewBuildEmulatorDefinition(
		dir []Directive,
		output string,
		scriptFilename string,
		createCallbackName string,
	) StarBuildDefinition
	NewBuildFsDefinition(
		dir []Directive,
		kind string,
	) BuildFSDefinition
	NewBuildVmDefinition(
		dir []Directive,
		kernel BuildDefinition,
		initramfs BuildDefinition,
		output string,
		cpuCores int,
		memoryMb int,
		autoScale bool,
		architecture config.CPUArchitecture,
		rootArchitecture config.CPUArchitecture,
		storageSize int,
		interaction string,
		debug bool,
	) BuildVmDefinition
	NewDecompressFileBuildDefinition(
		base BuildDefinition,
		kind string,
	) StarBuildDefinition
	NewExtractFileDefinition(
		base BuildDefinition,
		name string,
	) BuildDefinition
	NewFetchHttpBuildDefinition(
		url string,
		expireTime time.Duration,
		headers map[string]string,
	) StarBuildDefinition
	NewFetchOCIImageDefinition(
		registry, image, tag, architecture string,
	) FetchOciImageDefinition
	NewFetchCvmfsDefinition(
		mirror, repo, path string,
	) StarBuildDefinition
	NewDefinitionFromFile(
		f filesystem.File,
	) (BuildDefinition, error)
	NewConstantHashDefinition(
		hash string,
		builder BuilderFunc,
	) BuildDefinition
	NewPlanDefinition(
		builder string,
		arch config.CPUArchitecture,
		search []PackageQuery,
		tagList TagList,
	) (PlanDefinition, error)
	NewReadArchiveBuildDefinition(
		base BuildDefinition,
		kind string,
		stripComponents int,
	) ReadArchiveDefinition
	NewReadArchive2BuildDefinition(
		base BuildDefinition,
		kind string,
		stripComponents int,
	) ReadArchiveDefinition
	NewReadOCIImageDefinition(
		base BuildDefinition,
	) ReadOCIImageDefinition
	NewStarBuildDefinition(
		filename string,
		builder string,
		args []hash.SerializableValue,
	) (StarBuildDefinition, error)
}
