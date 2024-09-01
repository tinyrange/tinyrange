package database

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"go.starlark.net/starlark"
)

type ContainerBuilder struct {
	Name             string
	Architecture     config.CPUArchitecture
	DisplayName      string
	Filename         string
	PlanCallbackName string
	DefaultPackages  []common.PackageQuery
	Packages         *PackageCollection
	Metadata         starlark.Value
	db               common.PackageDatabase

	loaded bool
}

// Attr implements starlark.HasAttrs.
func (builder *ContainerBuilder) Attr(name string) (starlark.Value, error) {
	if name == "plan" {
		return starlark.NewBuiltin("ContainerBuilder.plan", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				val starlark.Value
				err error
			)

			var (
				searchListIt starlark.Iterable
				tagListIt    starlark.Iterable
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"packages", &searchListIt,
				"tags", &tagListIt,
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

			var tagList []string

			if tagListIt != nil {
				tagList, err = common.ToStringList(tagListIt)
				if err != nil {
					return starlark.None, err
				}
			}

			ctx := builder.db.NewBuildContext(nil)

			plan, err := builder.Plan(ctx, search, tagList, common.PlanOptions{})
			if err != nil {
				return nil, err
			}

			return plan, nil
		}), nil
	} else if name == "packages" {
		packages := make(map[string]*common.Package)
		for _, pkg := range builder.Packages.RawPackages {
			packages[pkg.Name.Key()] = pkg
		}

		var keys []string

		for k := range packages {
			keys = append(keys, k)
		}

		slices.Sort(keys)

		var ret []starlark.Value

		for _, k := range keys {
			ret = append(ret, packages[k])
		}

		return starlark.NewList(ret), nil
	} else if name == "metadata" {
		return builder.Metadata, nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (builder *ContainerBuilder) AttrNames() []string {
	return []string{"plan", "metadata"}
}

func (builder *ContainerBuilder) Loaded() bool {
	return builder.loaded
}

func (builder *ContainerBuilder) Load(ctx common.BuildContext) error {
	if builder.Loaded() {
		return nil
	}

	builder.db = ctx.Database()

	if err := builder.Packages.Load(ctx); err != nil {
		return err
	}

	builder.loaded = true

	return nil
}

func (builder *ContainerBuilder) Plan(
	ctx common.BuildContext,
	packages []common.PackageQuery,
	tags common.TagList,
	opts common.PlanOptions,
) (common.InstallationPlan, error) {
	plan := NewInstallationPlan(tags, opts)

	if tags.Contains("defaults") {
		for _, pkg := range builder.DefaultPackages {
			if err := plan.Add(ctx, builder, pkg); err != nil {
				return nil, err
			}
		}
	}

	// Add all the requested packages.
	for _, pkg := range packages {
		if err := plan.Add(ctx, builder, pkg); err != nil {
			return nil, err
		}
	}

	// If we are in debugging mode then skip making the plan.
	if opts.Debug {
		return plan, nil
	}

	// Call the plan callback.
	thread := ctx.Database().NewThread(builder.Filename)

	callable, err := ctx.Database().GetBuilder(builder.Filename, builder.PlanCallbackName)
	if err != nil {
		return nil, fmt.Errorf("could not get builder for ContainerBuilder.Plan: %s", err)
	}

	ret, err := starlark.Call(
		thread,
		callable,
		starlark.Tuple{builder, plan},
		[]starlark.Tuple{},
	)
	if err != nil {
		if sErr, ok := err.(*starlark.EvalError); ok {
			slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
		}
		return nil, err
	}

	iterable, ok := ret.(starlark.Iterable)
	if !ok {
		return nil, fmt.Errorf("could not convert plan result into a Iterable")
	}

	var directives []common.Directive

	it := iterable.Iterate()
	defer it.Done()

	var val starlark.Value

	for it.Next(&val) {
		dir, err := asDirective(val)
		if err != nil {
			return nil, err
		}

		directives = append(directives, dir)
	}

	plan.SetDirectives(directives)

	return plan, nil
}

func (builder *ContainerBuilder) Search(pkg common.PackageQuery) ([]*common.Package, error) {
	return builder.Packages.Query(pkg)
}

func (builder *ContainerBuilder) Get(key string) (*common.Package, bool) {
	pkg, ok := builder.Packages.RawPackages[key]
	return pkg, ok
}

func (builder *ContainerBuilder) String() string {
	return fmt.Sprintf("ContainerBuilder{%s}", builder.Packages)
}
func (*ContainerBuilder) Type() string { return "ContainerBuilder" }
func (*ContainerBuilder) Hash() (uint32, error) {
	return 0, fmt.Errorf("ContainerBuilder is not hashable")
}
func (*ContainerBuilder) Truth() starlark.Bool { return starlark.True }
func (*ContainerBuilder) Freeze()              {}

var (
	_ starlark.Value          = &ContainerBuilder{}
	_ starlark.HasAttrs       = &ContainerBuilder{}
	_ common.ContainerBuilder = &ContainerBuilder{}
)

func NewContainerBuilder(
	name string,
	arch config.CPUArchitecture,
	displayName string,
	filename string,
	planCallbackName string,
	defaultPackages []common.PackageQuery,
	packages *PackageCollection,
	metadata starlark.Value,
) (*ContainerBuilder, error) {
	return &ContainerBuilder{
		Name:             name,
		Architecture:     arch,
		DisplayName:      displayName,
		Filename:         filename,
		PlanCallbackName: planCallbackName,
		DefaultPackages:  defaultPackages,
		Packages:         packages,
		Metadata:         metadata,
	}, nil
}
