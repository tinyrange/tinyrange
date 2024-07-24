package database

import (
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"sync"
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

	RawPackages map[string]*common.Package
	Packages    map[string][]*common.Package

	pkgMtx sync.Mutex
}

// Build implements common.BuildDefinition.
func (parser *PackageCollection) Build(ctx common.BuildContext) (common.BuildResult, error) {
	panic("unimplemented")
}

// NeedsBuild implements common.BuildDefinition.
func (parser *PackageCollection) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	panic("unimplemented")
}

func (parser *PackageCollection) addPackage(pkg *common.Package) error {
	parser.pkgMtx.Lock()
	defer parser.pkgMtx.Unlock()

	parser.RawPackages[pkg.Name.Key()] = pkg

	parser.Packages[pkg.Name.Name] = append(parser.Packages[pkg.Name.Name], pkg)

	for _, alias := range pkg.Aliases {
		parser.Packages[alias.Name] = append(parser.Packages[alias.Name], pkg)
	}

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

	wg := sync.WaitGroup{}

	// This doesn't scale partially well but 4 threads gives roughly a 2x speed improvement.
	groupCount := min(runtime.NumCPU(), 4)
	groupSize := len(records) / groupCount

	done := make(chan bool)
	errors := make(chan error)

	for i := 0; i < len(records); i += groupSize {
		wg.Add(1)

		go func(records []starlark.Value) {
			defer wg.Done()

			child := ctx.ChildContext(parser, nil, "")

			_, err := child.Call(parser.Parser, parser, starlark.NewList(records))
			if err != nil {
				errors <- err
			}
		}(records[i:min(len(records), i+groupSize)])
	}

	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case err := <-errors:
		return err
	case <-done:
		slog.Info("loaded all packages", "count", len(records), "took", time.Since(start))

		return nil
	}
}

func (parser *PackageCollection) Query(query common.PackageQuery) ([]*common.Package, error) {
	var directs []*common.Package
	var aliases []*common.Package

	if query.MatchPartialName {
		for _, pkg := range parser.RawPackages {
			if pkg.Name.Matches(query) {
				directs = append(directs, pkg)
			} else if pkg.Matches(query) {
				aliases = append(aliases, pkg)
			}
		}
	} else {
		opts, ok := parser.Packages[query.Name]
		if !ok {
			return nil, nil
		}

		for _, pkg := range opts {
			if pkg.Name.Matches(query) {
				directs = append(directs, pkg)
			} else if pkg.Matches(query) {
				aliases = append(aliases, pkg)
			}
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
	_ starlark.Value         = &PackageCollection{}
	_ starlark.HasAttrs      = &PackageCollection{}
	_ common.BuildDefinition = &PackageCollection{}
)

func NewPackageCollection(
	name string,
	parser starlark.Callable,
	install starlark.Callable,
	sources []common.BuildDefinition,
) (*PackageCollection, error) {
	return &PackageCollection{
		Name:        []string{name, parser.Name(), install.Name()},
		Parser:      parser,
		Install:     install,
		Sources:     sources,
		Packages:    make(map[string][]*common.Package),
		RawPackages: make(map[string]*common.Package),
	}, nil
}
