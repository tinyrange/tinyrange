package builder

import (
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/feature"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

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
) common.BuildFSDefinition {
	return newBuildFsDefinition(dir, kind)
}

func (*definitionFactory) NewBuildVmDefinition(
	dir []common.Directive,
	kernel common.BuildDefinition,
	initramfs common.BuildDefinition,
	output string,
	cpuCores int,
	memoryMb int,
	autoScale bool,
	architecture config.CPUArchitecture,
	rootArchitecture config.CPUArchitecture,
	storageSize int,
	interaction string,
	debug bool,
) common.BuildVmDefinition {
	return newBuildVmDefinition(dir,
		kernel,
		initramfs,
		output,
		cpuCores, memoryMb, autoScale,
		architecture, rootArchitecture,
		storageSize,
		interaction,
		debug,
	)
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
) common.FetchOciImageDefinition {
	if feature.HasFeature(feature.FeatureOCIArchive2) {
		return newFetchOCIImageV2Definition(registry, image, tag, architecture)
	} else {
		return newFetchOCIImageDefinition(registry, image, tag, architecture)
	}
}

func (d *definitionFactory) NewFetchCvmfsDefinition(
	mirror string, repo string, path string,
) common.StarBuildDefinition {
	return newFetchCvmfsDefinition(mirror, repo, path)
}

func (*definitionFactory) NewDefinitionFromFile(
	f filesystem.File,
) (common.BuildDefinition, error) {
	return newDefinitionFromFile(f)
}

func (*definitionFactory) NewConstantHashDefinition(
	hash string,
	builder common.BuilderFunc,
) common.BuildDefinition {
	return newConstantHashDefinition(hash, builder)
}

func (*definitionFactory) NewPlanDefinition(
	builder string,
	arch config.CPUArchitecture,
	search []common.PackageQuery,
	tagList common.TagList,
) (common.PlanDefinition, error) {
	return newPlanDefinition(builder, arch, search, tagList)
}

func (*definitionFactory) NewReadArchiveBuildDefinition(
	base common.BuildDefinition,
	kind string,
	stripComponents int,
) common.ReadArchiveDefinition {
	return newReadArchiveBuildDefinition(base, kind, stripComponents)
}

func (*definitionFactory) NewReadOCIImageDefinition(
	base common.BuildDefinition,
) common.ReadOCIImageDefinition {
	return newReadOCIImageDefinition(base)
}

func (*definitionFactory) NewStarBuildDefinition(
	filename string,
	builder string,
	args []hash.SerializableValue,
) (common.StarBuildDefinition, error) {
	return newStarBuildDefinition(filename, builder, args)
}

var _ common.DefinitionFactory = &definitionFactory{}

var Factory common.DefinitionFactory = &definitionFactory{}
