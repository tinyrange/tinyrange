package builder

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&PlanDefinition{})
}

type PlanDefinition struct {
	params PlanParameters

	Fragments []config.Fragment
}

// Dependencies implements common.BuildDefinition.
func (def *PlanDefinition) Dependencies(ctx common.BuildContext) ([]common.DependencyNode, error) {
	// The builder is a dynamic dependency.
	return []common.DependencyNode{}, nil
}

// implements common.BuildDefinition.
func (def *PlanDefinition) Params() hash.SerializableValue { return def.params }
func (def *PlanDefinition) SerializableType() string       { return "PlanDefinition" }
func (def *PlanDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &PlanDefinition{params: params.(PlanParameters)}
}

// AsFragments implements common.Directive.
func (def *PlanDefinition) AsFragments(ctx common.BuildContext) ([]config.Fragment, error) {
	res, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

	if err := ParseJsonFromFile(res, &def); err != nil {
		return nil, err
	}

	return def.Fragments, nil
}

// ToStarlark implements common.BuildDefinition.
func (def *PlanDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	var plan *PlanDefinition

	if err := ParseJsonFromFile(result, &plan); err != nil {
		return nil, err
	}

	// Copy the parameters so the definition can be rebuilt.
	plan.params = def.params

	return plan, nil
}

// Attr implements starlark.HasAttrs.
func (def *PlanDefinition) Attr(name string) (starlark.Value, error) {
	if name == "filesystem" {
		return starlark.NewBuiltin("PlanDefinition.filesystem", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var commands []starlark.Value

			dir := filesystem.NewMemoryDirectory()

			for _, frag := range def.Fragments {
				if frag.Archive != nil {
					ark, err := filesystem.ReadArchiveFromFile(
						filesystem.NewLocalFile(frag.Archive.HostFilename, nil),
					)
					if err != nil {
						return starlark.None, err
					}

					if err := filesystem.ExtractArchive(ark, dir); err != nil {
						return starlark.None, err
					}
				} else if frag.RunCommand != nil {
					commands = append(commands, starlark.String(frag.RunCommand.Command))
				} else {
					return starlark.None, fmt.Errorf("unimplemented fragment type: %+v", frag)
				}
			}

			return starlark.Tuple{
				filesystem.NewStarDirectory(dir, ""),
				starlark.NewList(commands),
			}, nil
		}), nil
	} else if name == "add_packages" {
		return starlark.NewBuiltin("PlanDefinition.add_packages", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				val starlark.Value
			)

			var searchListIt starlark.Iterable

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"packages", &searchListIt,
			); err != nil {
				return starlark.None, err
			}

			var search []common.PackageQuery

			{
				dependencyIter := searchListIt.Iterate()
				defer dependencyIter.Done()

				for dependencyIter.Next(&val) {
					dep, ok := val.(common.PackageQuery)
					if !ok {
						return nil, fmt.Errorf("could not convert %s to PackageQuery", val.Type())
					}

					search = append(search, dep)
				}
			}

			return &PlanDefinition{
				params: PlanParameters{
					Builder:      def.params.Builder,
					Architecture: def.params.Architecture,
					Search:       append(def.params.Search, search...),
					TagList:      def.params.TagList,
				},
			}, nil
		}), nil
	} else if name == "with_packages" {
		return starlark.NewBuiltin("PlanDefinition.with_packages", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				val starlark.Value
			)

			var searchListIt starlark.Iterable

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"packages", &searchListIt,
			); err != nil {
				return starlark.None, err
			}

			var search []common.PackageQuery

			{
				dependencyIter := searchListIt.Iterate()
				defer dependencyIter.Done()

				for dependencyIter.Next(&val) {
					dep, ok := val.(common.PackageQuery)
					if !ok {
						return nil, fmt.Errorf("could not convert %s to PackageQuery", val.Type())
					}

					search = append(search, dep)
				}
			}

			return &PlanDefinition{
				params: PlanParameters{
					Builder:      def.params.Builder,
					Architecture: def.params.Architecture,
					Search:       search,
					TagList:      def.params.TagList,
				},
			}, nil
		}), nil
	} else if name == "set_tags" {
		return starlark.NewBuiltin("PlanDefinition.add_packages", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var tagListIt starlark.Iterable

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"tags", &tagListIt,
			); err != nil {
				return starlark.None, err
			}

			tagList, err := common.ToStringList(tagListIt)
			if err != nil {
				return starlark.None, err
			}

			return &PlanDefinition{
				params: PlanParameters{
					Builder:      def.params.Builder,
					Architecture: def.params.Architecture,
					Search:       def.params.Search,
					TagList:      tagList,
				},
			}, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (def *PlanDefinition) AttrNames() []string {
	return []string{"filesystem", "add_packages"}
}

// WriteTo implements common.BuildResult.
func (def *PlanDefinition) WriteResult(w io.Writer) error {
	enc := json.NewEncoder(w)

	if err := enc.Encode(&def); err != nil {
		return err
	}

	return nil
}

// Build implements common.BuildDefinition.
func (def *PlanDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	arch, err := config.ArchitectureFromString(def.params.Architecture)
	if err != nil {
		return nil, err
	}
	if arch == config.ArchInvalid {
		arch = config.HostArchitecture
	}

	builder, err := ctx.Database().GetContainerBuilder(ctx, def.params.Builder, arch)
	if err != nil {
		return nil, err
	}

	plan, err := builder.Plan(ctx, def.params.Search, def.params.TagList, common.PlanOptions{})
	if err != nil {
		plan, _ = builder.Plan(ctx, def.params.Search, def.params.TagList, common.PlanOptions{
			Debug: true,
		})

		plan.WriteTree()

		return nil, err
	}

	if err := plan.WriteTree(); err != nil {
		return nil, err
	}

	for _, dir := range plan.Directives() {
		frags, err := dir.AsFragments(ctx)
		if err != nil {
			return nil, err
		}

		def.Fragments = append(def.Fragments, frags...)
	}

	return def, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *PlanDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.Database().ShouldRebuildUserDefinitions() {
		return true, nil
	}

	return false, nil
}

// Tag implements common.BuildDefinition.
func (def *PlanDefinition) Tag() string {
	return strings.Join([]string{
		"PlanDefinition",
		def.params.Builder,
		fmt.Sprintf("%+v", def.params.Search),
		def.params.TagList.String(),
	}, "_")
}

func (def *PlanDefinition) AddPackage(name common.PackageQuery) (*PlanDefinition, error) {
	return &PlanDefinition{
		params: PlanParameters{
			Builder:      def.params.Builder,
			Architecture: def.params.Architecture,
			Search:       append(def.params.Search, name),
			TagList:      def.params.TagList,
		},
	}, nil
}

func (def *PlanDefinition) String() string { return def.Tag() }
func (*PlanDefinition) Type() string       { return "PlanDefinition" }
func (*PlanDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("PlanDefinition is not hashable")
}
func (*PlanDefinition) Truth() starlark.Bool { return starlark.True }
func (*PlanDefinition) Freeze()              {}

var (
	_ starlark.Value         = &PlanDefinition{}
	_ starlark.HasAttrs      = &PlanDefinition{}
	_ common.BuildDefinition = &PlanDefinition{}
	_ common.BuildResult     = &PlanDefinition{}
	_ common.Directive       = &PlanDefinition{}
)

func NewPlanDefinition(builder string, arch config.CPUArchitecture, search []common.PackageQuery, tagList common.TagList) (*PlanDefinition, error) {
	if builder == "" {
		return nil, fmt.Errorf("no builder specified")
	}

	return &PlanDefinition{
		params: PlanParameters{
			Builder:      builder,
			Architecture: string(arch),
			Search:       search,
			TagList:      tagList,
		},
	}, nil
}
