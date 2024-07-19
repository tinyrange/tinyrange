package database

import (
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/record"
	"go.starlark.net/starlark"
)

type PackageCollection struct {
	Name    []string
	Parser  starlark.Callable
	Install starlark.Callable
	Sources []common.BuildDefinition

	Packages map[string]*common.Package
}

func (parser *PackageCollection) addPackage(pkg *common.Package) error {
	parser.Packages[pkg.Name.String()] = pkg

	return nil
}

// Attr implements starlark.HasAttrs.
func (parser *PackageCollection) Attr(name string) (starlark.Value, error) {
	if name == "add_package" {
		return starlark.NewBuiltin("PackageCollection.add_package", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var val starlark.Value

			var (
				name      common.PackageName
				aliasList starlark.Iterable
				raw       starlark.Value
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
				"aliases?", &aliasList,
				"raw?", &raw,
			); err != nil {
				return starlark.None, err
			}

			var aliases []common.PackageName

			if aliasList != nil {
				iter := aliasList.Iterate()
				defer iter.Done()

				for iter.Next(&val) {
					alias, ok := val.(common.PackageName)
					if !ok {
						return nil, fmt.Errorf("could not convert %s to PackageName", val.Type())
					}

					aliases = append(aliases, alias)
				}
			}

			pkg := common.NewPackage(name, aliases, raw)

			if err := parser.addPackage(pkg); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (parser *PackageCollection) AttrNames() []string {
	return []string{"add_package"}
}

// Tag implements BuildSource.
func (parser *PackageCollection) Tag() string {
	return strings.Join(parser.Name, "_")
}

func (parser *PackageCollection) Load(db *PackageDatabase) error {
	var records []starlark.Value

	ctx := db.NewBuildContext(parser)

	start := time.Now()

	// Build all the package sources.
	// This can happen in parallel.
	for _, source := range parser.Sources {
		built, err := db.Build(ctx, source, common.BuildOptions{})
		if err != nil {
			return err
		}

		fh, err := built.Open()
		if err != nil {
			return err
		}

		reader := record.NewReader2(fh)

		for {
			record, err := reader.ReadValue()
			if err == io.EOF {
				break
			} else if err != nil {
				return err
			}

			records = append(records, record)
		}
	}

	slog.Info("built all package sources", "took", time.Since(start))
	start = time.Now()

	child := ctx.ChildContext(parser, nil, "")

	_, err := child.Call(parser.Parser, parser, starlark.NewList(records))
	if err != nil {
		return err
	}

	slog.Info("loaded all packages", "count", len(records), "took", time.Since(start))

	return nil
}

func (parser *PackageCollection) Query(query common.PackageQuery) ([]*common.Package, error) {
	var directs []*common.Package
	var aliases []*common.Package

	for _, pkg := range parser.Packages {
		if pkg.Name.Matches(query) {
			directs = append(directs, pkg)
		} else if pkg.Matches(query) {
			aliases = append(aliases, pkg)
		}
	}

	slices.SortFunc(directs, func(a *common.Package, b *common.Package) int {
		return strings.Compare(a.Name.String(), b.Name.String())
	})

	slices.SortFunc(aliases, func(a *common.Package, b *common.Package) int {
		return strings.Compare(a.Name.String(), b.Name.String())
	})

	return append(directs, aliases...), nil
}

func (parser *PackageCollection) InstallerFor(pkg *common.Package, tags common.TagList) (*common.Installer, error) {
	thread := &starlark.Thread{Name: pkg.Name.String()}

	ret, err := starlark.Call(thread, parser.Install, starlark.Tuple{pkg, tags}, []starlark.Tuple{})
	if err != nil {
		if sErr, ok := err.(*starlark.EvalError); ok {
			slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
		}

		return nil, err
	}

	install, ok := ret.(*common.Installer)
	if !ok {
		return nil, fmt.Errorf("could not convert %s to installer", ret.Type())
	}

	return install, nil
}

func (def *PackageCollection) String() string { return strings.Join(def.Name, "_") }
func (*PackageCollection) Type() string       { return "PackageCollection" }
func (*PackageCollection) Hash() (uint32, error) {
	return 0, fmt.Errorf("PackageCollection is not hashable")
}
func (*PackageCollection) Truth() starlark.Bool { return starlark.True }
func (*PackageCollection) Freeze()              {}

var (
	_ starlark.Value     = &PackageCollection{}
	_ starlark.HasAttrs  = &PackageCollection{}
	_ common.BuildSource = &PackageCollection{}
)

func NewPackageCollection(
	name string,
	parser starlark.Callable,
	install starlark.Callable,
	sources []common.BuildDefinition,
) (*PackageCollection, error) {
	return &PackageCollection{
		Name:     []string{name, parser.Name(), install.Name()},
		Parser:   parser,
		Install:  install,
		Sources:  sources,
		Packages: make(map[string]*common.Package),
	}, nil
}
