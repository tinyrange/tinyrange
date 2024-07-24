package builder

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/cpio"
	"go.starlark.net/starlark"
)

type initRamFsBuilderResult struct {
	frags []config.Fragment
}

// WriteTo implements common.BuildResult.
func (i *initRamFsBuilderResult) WriteTo(w io.Writer) (n int64, err error) {
	writer := cpio.New()

	for _, frag := range i.frags {
		if frag.Archive != nil {
			f := common.NewLocalFile(frag.Archive.HostFilename)

			ark, err := common.ReadArchiveFromFile(f)
			if err != nil {
				return 0, err
			}

			ents, err := ark.Entries()
			if err != nil {
				return 0, err
			}

			for _, ent := range ents {
				if err := writer.AddFromEntry(frag.Archive.Target, ent); err != nil {
					return 0, err
				}
			}
		} else if frag.FileContents != nil {
			c := frag.FileContents

			filename := strings.TrimPrefix(c.GuestFilename, "/")

			if err := writer.AddSimpleFile(filename, c.Contents, c.Executable); err != nil {
				return 0, fmt.Errorf("failed to add simple file: %s", c.GuestFilename)
			}
		} else {
			return 0, fmt.Errorf("unhandled fragment type: %+v", frag)
		}
	}

	return writer.WriteTo(w)
}

var (
	_ common.BuildResult = &initRamFsBuilderResult{}
)

type BuildFsDefinition struct {
	Directives []common.Directive
	Kind       string

	frags []config.Fragment
}

// Create implements common.BuildDefinition.
func (def *BuildFsDefinition) Create(params common.BuildDefinitionParameters) common.BuildDefinition {
	panic("unimplemented")
}

// Params implements common.BuildDefinition.
func (def *BuildFsDefinition) Params() common.BuildDefinitionParameters {
	panic("unimplemented")
}

// Build implements common.BuildDefinition.
func (def *BuildFsDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	// Launch child builds for each directive.
	for _, directive := range def.Directives {
		frags, err := directiveToFragments(ctx, directive)
		if err != nil {
			return nil, err
		}

		for _, frag := range frags {
			if frag.RunCommand != nil {
				return nil, fmt.Errorf("build_fs does not support running commands")
			} else {
				def.frags = append(def.frags, frag)
			}
		}
	}

	if def.Kind == "initramfs" {
		return &initRamFsBuilderResult{frags: def.frags}, nil
	} else {
		return nil, fmt.Errorf("kind not implemented: %s", def.Kind)
	}
}

// NeedsBuild implements common.BuildDefinition.
func (def *BuildFsDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.Database().ShouldRebuildUserDefinitions() {
		return true, nil
	}

	// TODO(joshua): Check if any of the child directives
	return false, nil
}

func (def *BuildFsDefinition) String() string { return "BuildFsDefinition" }
func (*BuildFsDefinition) Type() string       { return "BuildFsDefinition" }
func (*BuildFsDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("BuildFsDefinition is not hashable")
}
func (*BuildFsDefinition) Truth() starlark.Bool { return starlark.True }
func (*BuildFsDefinition) Freeze()              {}

var (
	_ starlark.Value         = &BuildFsDefinition{}
	_ common.BuildDefinition = &BuildFsDefinition{}
)

func NewBuildFsDefinition(dir []common.Directive, kind string) *BuildFsDefinition {
	return &BuildFsDefinition{Directives: dir, Kind: kind}
}
