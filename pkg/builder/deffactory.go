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
	common.StarBuildDefinition1

	SetBuildTemplateMode()
}

type FetchOciImageDefinition interface {
	common.StarBuildDefinition1
	common.Directive
}

type ReadOCIImageDefinition interface {
	common.BuildDefinition1
	common.Directive
}

type BuilderFunc func() (io.ReadCloser, error)

type DefinitionFactory interface {
	NewBuildEmulatorDefinition(
		dir []common.Directive,
		output string,
		scriptFilename string,
		createCallbackName string,
	) common.StarBuildDefinition1
	NewBuildFsDefinition(
		dir []common.Directive,
		kind string,
	) common.StarBuildDefinition1
	NewBuildVmDefinition(
		dir []common.Directive,
		kernel common.BuildDefinition1,
		initramfs common.BuildDefinition1,
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
		base common.BuildDefinition1,
		kind string,
	) common.StarBuildDefinition1
	NewExtractFileDefinition(
		base common.BuildDefinition1,
		name string,
	) common.BuildDefinition1
	NewFetchHttpBuildDefinition(
		url string,
		expireTime time.Duration,
		headers map[string]string,
	) common.StarBuildDefinition1
	NewFetchOCIImageDefinition(
		registry, image, tag, architecture string,
	) FetchOciImageDefinition
	NewDefinitionFromFile(
		f filesystem.File,
	) (common.BuildDefinition1, error)
	NewConstantHashDefinition(
		hash string,
		builder BuilderFunc,
	) common.BuildDefinition1
	NewPlanDefinition(
		builder string,
		arch config.CPUArchitecture,
		search []common.PackageQuery,
		tagList common.TagList,
	) (PlanDefinition, error)
	NewReadArchiveBuildDefinition(
		base common.BuildDefinition1,
		kind string,
	) common.StarBuildDefinition1
	NewReadOCIImageDefinition(
		base common.BuildDefinition1,
	) ReadOCIImageDefinition
	NewStarBuildDefinition(
		filename string,
		builder string,
		args []hash.SerializableValue,
	) (common.StarBuildDefinition1, error)
}

type definitionFactory struct {
}

func (*definitionFactory) NewBuildEmulatorDefinition(
	dir []common.Directive,
	output string,
	scriptFilename string,
	createCallbackName string,
) common.StarBuildDefinition1 {
	return newBuildEmulatorDefinition(dir, output, scriptFilename, createCallbackName)
}

func (*definitionFactory) NewBuildFsDefinition(
	dir []common.Directive,
	kind string,
) common.StarBuildDefinition1 {
	return newBuildFsDefinition(dir, kind)
}

func (*definitionFactory) NewBuildVmDefinition(
	dir []common.Directive,
	kernel common.BuildDefinition1,
	initramfs common.BuildDefinition1,
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
	base common.BuildDefinition1,
	kind string,
) common.StarBuildDefinition1 {
	return newDecompressFileBuildDefinition(base, kind)
}

func (*definitionFactory) NewExtractFileDefinition(
	base common.BuildDefinition1,
	name string,
) common.BuildDefinition1 {
	return newExtractFileDefinition(base, name)
}

func (*definitionFactory) NewFetchHttpBuildDefinition(
	url string,
	expireTime time.Duration,
	headers map[string]string,
) common.StarBuildDefinition1 {
	return newFetchHttpBuildDefinition(url, expireTime, headers)
}

func (*definitionFactory) NewFetchOCIImageDefinition(
	registry, image, tag, architecture string,
) FetchOciImageDefinition {
	return newFetchOCIImageDefinition(registry, image, tag, architecture)
}

func (*definitionFactory) NewDefinitionFromFile(
	f filesystem.File,
) (common.BuildDefinition1, error) {
	return newDefinitionFromFile(f)
}

func (*definitionFactory) NewConstantHashDefinition(
	hash string,
	builder BuilderFunc,
) common.BuildDefinition1 {
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
	base common.BuildDefinition1,
	kind string,
) common.StarBuildDefinition1 {
	return newReadArchiveBuildDefinition(base, kind)
}

func (*definitionFactory) NewReadOCIImageDefinition(
	base common.BuildDefinition1,
) ReadOCIImageDefinition {
	return newReadOCIImageDefinition(base)
}

func (*definitionFactory) NewStarBuildDefinition(
	filename string,
	builder string,
	args []hash.SerializableValue,
) (common.StarBuildDefinition1, error) {
	return newStarBuildDefinition(filename, builder, args)
}

var _ DefinitionFactory = &definitionFactory{}

var Factory DefinitionFactory = &definitionFactory{}
