package builder

import (
	"io"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

type BuildVmDefinition interface {
	common.StarBuildDefinition
	common.Directive

	SetBuildTemplateMode()
}

type FetchOciImageDefinition interface {
	common.StarBuildDefinition
	common.Directive
}

type ReadOCIImageDefinition interface {
	common.BuildDefinition
	common.Directive
}

type BuildFSDefinition interface {
	common.StarBuildDefinition
	common.Directive
}

type ReadArchiveDefinition interface {
	common.StarBuildDefinition
	common.Directive
}

type BuilderFunc func() (io.ReadCloser, error)

type DefinitionFactory interface {
	NewBuildEmulatorDefinition(
		dir []common.Directive,
		output string,
		scriptFilename string,
		createCallbackName string,
	) common.StarBuildDefinition
	NewBuildFsDefinition(
		dir []common.Directive,
		kind string,
	) BuildFSDefinition
	NewBuildVmDefinition(
		dir []common.Directive,
		kernel common.BuildDefinition,
		initramfs common.BuildDefinition,
		output string,
		cpuCores int,
		memoryMb int,
		architecture config.CPUArchitecture,
		rootArchitecture config.CPUArchitecture,
		storageSize int,
		interaction string,
		debug bool,
	) BuildVmDefinition
	NewDecompressFileBuildDefinition(
		base common.BuildDefinition,
		kind string,
	) common.StarBuildDefinition
	NewExtractFileDefinition(
		base common.BuildDefinition,
		name string,
	) common.BuildDefinition
	NewFetchHttpBuildDefinition(
		url string,
		expireTime time.Duration,
		headers map[string]string,
	) common.StarBuildDefinition
	NewFetchOCIImageDefinition(
		registry, image, tag, architecture string,
	) FetchOciImageDefinition
	NewDefinitionFromFile(
		f filesystem.File,
	) (common.BuildDefinition, error)
	NewConstantHashDefinition(
		hash string,
		builder BuilderFunc,
	) common.BuildDefinition
	NewPlanDefinition(
		builder string,
		arch config.CPUArchitecture,
		search []common.PackageQuery,
		tagList common.TagList,
	) (PlanDefinition, error)
	NewReadArchiveBuildDefinition(
		base common.BuildDefinition,
		kind string,
	) ReadArchiveDefinition
	NewReadOCIImageDefinition(
		base common.BuildDefinition,
	) ReadOCIImageDefinition
	NewStarBuildDefinition(
		filename string,
		builder string,
		args []hash.SerializableValue,
	) (common.StarBuildDefinition, error)
}

type definitionFactory struct {
}

func (*definitionFactory) NewBuildEmulatorDefinition(
	dir []common.Directive,
	output string,
	scriptFilename string,
	createCallbackName string,
) common.StarBuildDefinition {
	return newBuildEmulatorDefinition(dir, output, scriptFilename, createCallbackName)
}

func (*definitionFactory) NewBuildFsDefinition(
	dir []common.Directive,
	kind string,
) BuildFSDefinition {
	return newBuildFsDefinition(dir, kind)
}

func (*definitionFactory) NewBuildVmDefinition(
	dir []common.Directive,
	kernel common.BuildDefinition,
	initramfs common.BuildDefinition,
	output string,
	cpuCores int,
	memoryMb int,
	architecture config.CPUArchitecture,
	rootArchitecture config.CPUArchitecture,
	storageSize int,
	interaction string,
	debug bool,
) BuildVmDefinition {
	return newBuildVmDefinition(dir, kernel, initramfs, output, cpuCores, memoryMb, architecture, rootArchitecture, storageSize, interaction, debug)
}

func (*definitionFactory) NewDecompressFileBuildDefinition(
	base common.BuildDefinition,
	kind string,
) common.StarBuildDefinition {
	return newDecompressFileBuildDefinition(base, kind)
}

func (*definitionFactory) NewExtractFileDefinition(
	base common.BuildDefinition,
	name string,
) common.BuildDefinition {
	return newExtractFileDefinition(base, name)
}

func (*definitionFactory) NewFetchHttpBuildDefinition(
	url string,
	expireTime time.Duration,
	headers map[string]string,
) common.StarBuildDefinition {
	return newFetchHttpBuildDefinition(url, expireTime, headers)
}

func (*definitionFactory) NewFetchOCIImageDefinition(
	registry, image, tag, architecture string,
) FetchOciImageDefinition {
	return newFetchOCIImageDefinition(registry, image, tag, architecture)
}

func (*definitionFactory) NewDefinitionFromFile(
	f filesystem.File,
) (common.BuildDefinition, error) {
	return newDefinitionFromFile(f)
}

func (*definitionFactory) NewConstantHashDefinition(
	hash string,
	builder BuilderFunc,
) common.BuildDefinition {
	return newConstantHashDefinition(hash, builder)
}

func (*definitionFactory) NewPlanDefinition(
	builder string,
	arch config.CPUArchitecture,
	search []common.PackageQuery,
	tagList common.TagList,
) (PlanDefinition, error) {
	return newPlanDefinition(builder, arch, search, tagList)
}

func (*definitionFactory) NewReadArchiveBuildDefinition(
	base common.BuildDefinition,
	kind string,
) ReadArchiveDefinition {
	return newReadArchiveBuildDefinition(base, kind)
}

func (*definitionFactory) NewReadOCIImageDefinition(
	base common.BuildDefinition,
) ReadOCIImageDefinition {
	return newReadOCIImageDefinition(base)
}

func (*definitionFactory) NewStarBuildDefinition(
	filename string,
	builder string,
	args []hash.SerializableValue,
) (common.StarBuildDefinition, error) {
	return newStarBuildDefinition(filename, builder, args)
}

var _ DefinitionFactory = &definitionFactory{}

var Factory DefinitionFactory = &definitionFactory{}
