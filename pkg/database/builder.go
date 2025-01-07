package database

import (
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"go.starlark.net/starlark"
)

type containerBuilder struct {
	name                 string
	architecture         config.CPUArchitecture
	displayName          string
	filename             string
	planCallbackName     string
	defaultPackages      []common.PackageQuery
	packages             *packageCollection
	metadata             starlark.Value
	splitDefaultPackages bool
	db                   common.PackageDatabase

	loaded bool
}

// Packages returns the package collection of the container builder.
func (builder *containerBuilder) Packages() common.PackageCollection {
	return builder.packages
}

// EnsureLoaded ensures that the container builder is loaded.
func (builder *containerBuilder) EnsureLoaded(ctx common.BuildContext) error {
	if !builder.Loaded() {
		start := time.Now()
		if err := builder.Load(ctx); err != nil {
			return err
		}
		slog.Debug("loaded", "builder", builder.displayName, "arch", builder.architecture, "took", time.Since(start))
	}

	return nil
}

// Key returns the key of the container builder.
func (builder *containerBuilder) Key() string {
	return fmt.Sprintf("%s-%s", builder.name, builder.architecture)
}

// DisplayName returns the display name of the container builder.
func (builder *containerBuilder) DisplayName() string {
	return builder.displayName
}

// Attr implements starlark.HasAttrs.
func (builder *containerBuilder) Attr(name string) (starlark.Value, error) {
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
		for _, pkg := range builder.packages.RawPackages {
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
		return builder.metadata, nil
	} else if name == "arch" {
		return starlark.String(builder.architecture), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (builder *containerBuilder) AttrNames() []string {
	return []string{"plan", "metadata", "arch"}
}

func (builder *containerBuilder) Loaded() bool {
	return builder.loaded
}

func (builder *containerBuilder) Load(ctx common.BuildContext) error {
	if builder.Loaded() {
		return nil
	}

	builder.db = ctx.Database()

	if err := builder.packages.Load(ctx); err != nil {
		return err
	}

	builder.loaded = true

	return nil
}

func (builder *containerBuilder) Plan(
	ctx common.BuildContext,
	packages []common.PackageQuery,
	tags common.TagList,
	opts common.PlanOptions,
) (common.InstallationPlan, error) {
	plan := NewInstallationPlan(tags, opts)

	if tags.Contains("defaults") {
		for _, pkg := range builder.defaultPackages {
			if err := plan.Add(ctx, builder, pkg, builder.splitDefaultPackages); err != nil {
				return nil, err
			}
		}
	}

	// Add all the requested packages.
	for _, pkg := range packages {
		if err := plan.Add(ctx, builder, pkg, false); err != nil {
			return nil, err
		}
	}

	// If we are in debugging mode then skip making the plan.
	if opts.Debug {
		return plan, nil
	}

	// Call the plan callback.
	thread := ctx.Database().NewThread(builder.filename)

	callable, err := ctx.Database().GetBuilder(builder.filename, builder.planCallbackName)
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

func (builder *containerBuilder) Search(pkg common.PackageQuery) ([]*common.Package, error) {
	return builder.packages.Query(pkg)
}

func (builder *containerBuilder) Get(key string) (*common.Package, bool) {
	pkg, ok := builder.packages.RawPackages[key]
	return pkg, ok
}

func (builder *containerBuilder) String() string {
	return fmt.Sprintf("ContainerBuilder{%s}", builder.packages)
}
func (*containerBuilder) Type() string { return "ContainerBuilder" }
func (*containerBuilder) Hash() (uint32, error) {
	return 0, fmt.Errorf("ContainerBuilder is not hashable")
}
func (*containerBuilder) Truth() starlark.Bool { return starlark.True }
func (*containerBuilder) Freeze()              {}

var (
	_ starlark.Value          = &containerBuilder{}
	_ starlark.HasAttrs       = &containerBuilder{}
	_ common.ContainerBuilder = &containerBuilder{}
)

func NewContainerBuilder(
	name string,
	arch config.CPUArchitecture,
	displayName string,
	filename string,
	planCallbackName string,
	defaultPackages []common.PackageQuery,
	packages *packageCollection,
	metadata starlark.Value,
	splitDefaultPackages bool,
) (common.ContainerBuilder, error) {
	return &containerBuilder{
		name:                 name,
		architecture:         arch,
		displayName:          displayName,
		filename:             filename,
		planCallbackName:     planCallbackName,
		defaultPackages:      defaultPackages,
		packages:             packages,
		metadata:             metadata,
		splitDefaultPackages: splitDefaultPackages,
	}, nil
}
