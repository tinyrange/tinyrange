package builder

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&fileDefinition{})
}

type copyFileResult struct {
	fh io.ReadCloser
}

// WriteTo implements common.BuildResult.
func (def *copyFileResult) WriteResult(w io.Writer) error {
	defer def.fh.Close()

	if _, err := io.Copy(w, def.fh); err != nil {
		return err
	}

	return nil
}

var (
	_ common.BuildResult = &copyFileResult{}
)

type fileDefinition struct {
	params FileParameters
}

// implements common.BuildDefinition.
func (def *fileDefinition) Params() hash.SerializableValue { return def.params }
func (def *fileDefinition) SerializableType() string {
	return "FileDefinition"
}
func (def *fileDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &fileDefinition{params: params.(FileParameters)}
}

// AsFragments implements common.Directive.
func (def *fileDefinition) AsFragments(ctx common.BuildContext1, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	art, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

	res, err := art.Default()
	if err != nil {
		return nil, err
	}

	digest, err := ctx.DigestFromFile(res)
	if err != nil {
		return nil, err
	}

	filename, err := ctx.FilenameFromDigest(digest)
	if err != nil {
		return nil, err
	}

	stat, err := def.params.File.Stat()
	if err != nil {
		return nil, err
	}

	return []config.Fragment{
		{LocalFile: &config.LocalFileFragment{
			HostFilename:  filename,
			GuestFilename: stat.Name(),
			Executable:    stat.Mode().Perm()&0111 != 0,
		}},
	}, nil
}

// ToStarlark implements common.BuildDefinition.
func (def *fileDefinition) ToStarlark(ctx common.BuildContext1, artifact common.BuildArtifact) (starlark.Value, error) {
	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	return filesystem.NewStarFile(result, artifact.Hash().String()), nil
}

// Build implements common.BuildDefinition.
func (def *fileDefinition) Build(ctx common.BuildContext1) (common.BuildResult, error) {
	fh, err := def.params.File.Open()
	if err != nil {
		return nil, err
	}

	return &copyFileResult{fh: fh}, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *fileDefinition) NeedsBuild(ctx common.BuildContext1) (bool, error) {
	info, err := def.params.File.Stat()
	if err != nil {
		return true, err
	}

	return info.ModTime().After(ctx.LastBuild()), nil
}

// Tag implements common.BuildDefinition.
func (def *fileDefinition) Tag() string {
	info, err := def.params.File.Stat()
	if err != nil {
		return "<unknown>"
	}

	return strings.Join([]string{info.Name()}, "_")
}

func (def *fileDefinition) String() string { return def.Tag() }
func (*fileDefinition) Type() string       { return "FileDefinition" }
func (*fileDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("FileDefinition is not hashable")
}
func (*fileDefinition) Truth() starlark.Bool { return starlark.True }
func (*fileDefinition) Freeze()              {}

var (
	_ starlark.Value          = &fileDefinition{}
	_ common.BuildDefinition1 = &fileDefinition{}
	_ common.Directive        = &fileDefinition{}
)

type constantHashDefinition struct {
	params  ConstantHashParameters
	builder BuilderFunc
}

// implements common.BuildDefinition.
func (c *constantHashDefinition) Params() hash.SerializableValue { return c.params }
func (c *constantHashDefinition) SerializableType() string       { return "ConstantHashDefinition" }
func (c *constantHashDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &constantHashDefinition{params: params.(ConstantHashParameters)}
}

// Build implements common.BuildDefinition.
func (c *constantHashDefinition) Build(ctx common.BuildContext1) (common.BuildResult, error) {
	if c.builder == nil {
		return nil, fmt.Errorf("no builder for ConstantHashDefinition(%s)", c.params.Hash)
	}

	r, err := c.builder()
	if err != nil {
		return nil, err
	}

	return &copyFileResult{fh: r}, nil
}

// NeedsBuild implements common.BuildDefinition.
func (c *constantHashDefinition) NeedsBuild(ctx common.BuildContext1) (bool, error) {
	return false, nil
}

// Tag implements common.BuildDefinition.
func (c *constantHashDefinition) Tag() string { return c.params.Hash }

// ToStarlark implements common.BuildDefinition.
func (c *constantHashDefinition) ToStarlark(ctx common.BuildContext1, artifact common.BuildArtifact) (starlark.Value, error) {
	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	return filesystem.NewStarFile(result, c.params.Hash), nil
}

var (
	_ common.BuildDefinition1 = &constantHashDefinition{}
)

func definitionFromSource(source hash.SerializableValue) (common.BuildDefinition1, error) {
	if def, ok := source.(common.BuildDefinition1); ok {
		return def, nil
	} else if child, ok := source.(filesystem.ChildSource); ok {
		base, err := definitionFromSource(child.Source)
		if err != nil {
			return nil, err
		}

		return newExtractFileDefinition(base, child.Name), nil
	} else {
		return nil, fmt.Errorf("NewDefinitionFromFile: unimplemented Source: %T %+v", source, source)
	}
}

func newDefinitionFromFile(f filesystem.File) (common.BuildDefinition1, error) {
	if source, err := filesystem.SourceFromFile(f); err == nil {
		return definitionFromSource(source)
	} else {
		slog.Warn("failed to get source from file", "err", err)
	}

	return &fileDefinition{params: FileParameters{File: f}}, nil
}

func SourceFromArchive(archive filesystem.Archive) (hash.SerializableValue, error) {
	return filesystem.SourceFromArchive(archive)
}

func newConstantHashDefinition(hash string, builder BuilderFunc) common.BuildDefinition1 {
	return &constantHashDefinition{params: ConstantHashParameters{Hash: hash}, builder: builder}
}
