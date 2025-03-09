package builder

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/star"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&planDefinition{})
}

type planDefinition struct {
	params PlanParameters

	Fragments []config.Fragment
}

// Dependencies implements common.BuildDefinition.
func (def *planDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return nil, nil
}

// implements common.BuildDefinition.
func (def *planDefinition) Params() hash.SerializableValue { return def.params }
func (def *planDefinition) SerializableType() string       { return "PlanDefinition" }
func (def *planDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &planDefinition{params: params.(PlanParameters)}
}

// AsFragments implements common.Directive.
func (def *planDefinition) AsFragments(ctx common.BuildContext, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	art, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

	res, err := art.Default()
	if err != nil {
		return nil, err
	}

	if err := ParseJsonFromFile(res, &def); err != nil {
		return nil, err
	}

	var ret []config.Fragment

	for _, frag := range def.Fragments {
		if interactive := frag.DefaultInteractive; interactive != nil {
			if special.DefaultInteractive != nil {
				if err := special.DefaultInteractive(
					common.DirectiveDefaultInteractive{InteractiveCommand: interactive.Args},
				); err != nil {
					return nil, err
				}
			}
		} else {
			ret = append(ret, frag)
		}
	}

	return ret, nil
}

// ToStarlark implements common.BuildDefinition.
func (def *planDefinition) ToStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	var plan *planDefinition

	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	if err := ParseJsonFromFile(result, &plan); err != nil {
		return nil, err
	}

	// Copy the parameters so the definition can be rebuilt.
	plan.params = def.params

	return plan, nil
}

// Attr implements starlark.HasAttrs.
func (def *planDefinition) Attr(name string) (starlark.Value, error) {
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
					ark, err := archive.ReadArchiveFromFile(
						filesystem.NewLocalFile(frag.Archive.HostFilename, nil),
					)
					if err != nil {
						return starlark.None, err
					}

					if err := archive.ExtractArchive(ark, dir); err != nil {
						return starlark.None, err
					}
				} else if frag.RunCommand != nil {
					commands = append(commands, starlark.String(frag.RunCommand.Command))
				} else {
					return starlark.None, fmt.Errorf("unimplemented fragment type: %+v", frag)
				}
			}

			return starlark.Tuple{
				star.NewStarDirectory(dir, ""),
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

			return &planDefinition{
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

			return &planDefinition{
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

			return &planDefinition{
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
func (def *planDefinition) AttrNames() []string {
	return []string{"filesystem", "add_packages"}
}

// WriteTo implements common.BuildResult.
func (def *planDefinition) WriteResult(w io.Writer) error {
	enc := json.NewEncoder(w)

	if err := enc.Encode(&def); err != nil {
		return err
	}

	return nil
}

// Build implements common.BuildDefinition.
func (def *planDefinition) Build(ctx common.BuildContext) error {
	arch, err := config.ArchitectureFromString(def.params.Architecture)
	if err != nil {
		return err
	}
	if arch == config.ArchInvalid {
		arch = config.HostArchitecture
	}

	builder, err := ctx.Database().GetContainerBuilder(def.params.Builder, arch)
	if err != nil {
		return err
	}

	plan, err := builder.Plan(ctx, def.params.Search, def.params.TagList, common.PlanOptions{})
	if err != nil {
		plan, _ = builder.Plan(ctx, def.params.Search, def.params.TagList, common.PlanOptions{
			Debug: true,
		})

		plan.WriteTree(os.Stderr)

		return err
	}

	if err := plan.WriteTree(os.Stderr); err != nil {
		return err
	}

	for _, dir := range plan.Directives() {
		frags, err := dir.AsFragments(ctx, common.SpecialDirectiveHandlers{})
		if err != nil {
			return err
		}

		def.Fragments = append(def.Fragments, frags...)
	}

	return ctx.WriteDefault(def)
}

// NeedsBuild implements common.BuildDefinition.
func (def *planDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	if ctx.ShouldRebuildUserDefinitions() {
		return true, nil
	}

	return false, nil
}

// Tag implements common.BuildDefinition.
func (def *planDefinition) Tag() string {
	return strings.Join([]string{
		"PlanDefinition",
		def.params.Builder,
		fmt.Sprintf("%+v", def.params.Search),
		def.params.TagList.String(),
	}, "_")
}

func (def *planDefinition) AddPackage(name common.PackageQuery) (common.PlanDefinition, error) {
	return &planDefinition{
		params: PlanParameters{
			Builder:      def.params.Builder,
			Architecture: def.params.Architecture,
			Search:       append(def.params.Search, name),
			TagList:      def.params.TagList,
		},
	}, nil
}

func (def *planDefinition) String() string { return def.Tag() }
func (*planDefinition) Type() string       { return "PlanDefinition" }
func (*planDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("PlanDefinition is not hashable")
}
func (*planDefinition) Truth() starlark.Bool { return starlark.True }
func (*planDefinition) Freeze()              {}

var (
	_ starlark.Value         = &planDefinition{}
	_ starlark.HasAttrs      = &planDefinition{}
	_ common.BuildDefinition = &planDefinition{}
	_ common.BuildResult     = &planDefinition{}
	_ common.Directive       = &planDefinition{}
)

func newPlanDefinition(builder string, arch config.CPUArchitecture, search []common.PackageQuery, tagList common.TagList) (common.PlanDefinition, error) {
	if builder == "" {
		return nil, fmt.Errorf("no builder specified")
	}

	return &planDefinition{
		params: PlanParameters{
			Builder:      builder,
			Architecture: string(arch),
			Search:       search,
			TagList:      tagList,
		},
	}, nil
}
