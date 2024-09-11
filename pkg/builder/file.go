package builder

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&FileDefinition{})
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

type FileDefinition struct {
	params FileParameters
}

// Dependencies implements common.BuildDefinition.
func (def *FileDefinition) Dependencies(ctx common.BuildContext) ([]common.DependencyNode, error) {
	return []common.DependencyNode{}, nil
}

// implements common.BuildDefinition.
func (def *FileDefinition) Params() hash.SerializableValue { return def.params }
func (def *FileDefinition) SerializableType() string {
	return "FileDefinition"
}
func (def *FileDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &FileDefinition{params: params.(FileParameters)}
}

// AsFragments implements common.Directive.
func (def *FileDefinition) AsFragments(ctx common.BuildContext) ([]config.Fragment, error) {
	res, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

	digest := res.Digest()

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
func (def *FileDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	return filesystem.NewStarFile(result, def.Tag()), nil
}

// Build implements common.BuildDefinition.
func (def *FileDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	fh, err := def.params.File.Open()
	if err != nil {
		return nil, err
	}

	return &copyFileResult{fh: fh}, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *FileDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	info, err := def.params.File.Stat()
	if err != nil {
		return true, err
	}

	return info.ModTime().After(cacheTime), nil
}

// Tag implements common.BuildDefinition.
func (def *FileDefinition) Tag() string {
	info, err := def.params.File.Stat()
	if err != nil {
		return "<unknown>"
	}

	return strings.Join([]string{info.Name()}, "_")
}

func (def *FileDefinition) String() string { return def.Tag() }
func (*FileDefinition) Type() string       { return "FileDefinition" }
func (*FileDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("FileDefinition is not hashable")
}
func (*FileDefinition) Truth() starlark.Bool { return starlark.True }
func (*FileDefinition) Freeze()              {}

var (
	_ starlark.Value         = &FileDefinition{}
	_ common.BuildDefinition = &FileDefinition{}
	_ common.Directive       = &FileDefinition{}
)

type BuilderFunc func() (io.ReadCloser, error)

type ConstantHashDefinition struct {
	params  ConstantHashParameters
	builder BuilderFunc
}

// Dependencies implements common.BuildDefinition.
func (c *ConstantHashDefinition) Dependencies(ctx common.BuildContext) ([]common.DependencyNode, error) {
	return []common.DependencyNode{}, nil
}

// implements common.BuildDefinition.
func (c *ConstantHashDefinition) Params() hash.SerializableValue { return c.params }
func (c *ConstantHashDefinition) SerializableType() string       { return "ConstantHashDefinition" }
func (c *ConstantHashDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &ConstantHashDefinition{params: params.(ConstantHashParameters)}
}

// Build implements common.BuildDefinition.
func (c *ConstantHashDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
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
func (c *ConstantHashDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	return false, nil
}

// Tag implements common.BuildDefinition.
func (c *ConstantHashDefinition) Tag() string { return c.params.Hash }

// ToStarlark implements common.BuildDefinition.
func (c *ConstantHashDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	return filesystem.NewStarFile(result, c.params.Hash), nil
}

var (
	_ common.BuildDefinition = &ConstantHashDefinition{}
)

func definitionFromSource(source hash.SerializableValue) (common.BuildDefinition, error) {
	if def, ok := source.(common.BuildDefinition); ok {
		return def, nil
	} else if child, ok := source.(filesystem.ChildSource); ok {
		base, err := definitionFromSource(child.Source)
		if err != nil {
			return nil, err
		}

		return NewExtractFileDefinition(base, child.Name), nil
	} else {
		return nil, fmt.Errorf("NewDefinitionFromFile: unimplemented Source: %T %+v", source, source)
	}
}

func NewDefinitionFromFile(f filesystem.File) (common.BuildDefinition, error) {
	if source, err := filesystem.SourceFromFile(f); err == nil {
		return definitionFromSource(source)
	} else {
		slog.Warn("failed to get source from file", "err", err)
	}

	return &FileDefinition{params: FileParameters{File: f}}, nil
}

func NewConstantHashDefinition(hash string, builder BuilderFunc) *ConstantHashDefinition {
	return &ConstantHashDefinition{params: ConstantHashParameters{Hash: hash}, builder: builder}
}
