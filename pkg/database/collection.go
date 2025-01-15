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
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/record"
	"go.starlark.net/starlark"
)

type packageCollection struct {
	Filename string
	Parser   string
	Install  string
	Sources  []common.BuildDefinition1

	RawPackages map[string]*common.Package
	Packages    map[string][]*common.Package

	pkgMtx sync.Mutex
}

// Build implements common.BuildDefinition.
func (parser *packageCollection) Build(ctx common.BuildContext1) (common.BuildResult, error) {
	panic("unimplemented on packageCollection")
}

// Create implements common.BuildDefinition.
func (parser *packageCollection) Create(params hash.SerializableValue) hash.Definition {
	panic("unimplemented on packageCollection")
}

// NeedsBuild implements common.BuildDefinition.
func (parser *packageCollection) NeedsBuild(ctx common.BuildContext1) (bool, error) {
	panic("unimplemented on packageCollection")
}

// Params implements common.BuildDefinition.
func (parser *packageCollection) Params() hash.SerializableValue {
	panic("unimplemented on packageCollection")
}

// SerializableType implements common.BuildDefinition.
func (parser *packageCollection) SerializableType() string {
	panic("unimplemented on packageCollection")
}

// ToStarlark implements common.BuildDefinition.
func (parser *packageCollection) ToStarlark(ctx common.BuildContext1, result filesystem.File) (starlark.Value, error) {
	panic("unimplemented on packageCollection")
}

func (parser *packageCollection) addPackage(pkg *common.Package) error {
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
func (parser *packageCollection) Attr(name string) (starlark.Value, error) {
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
				tagsValue starlark.Iterable
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
				"aliases?", &aliasList,
				"raw?", &raw,
				"tags?", &tagsValue,
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

			var tags common.TagList
			var err error
			if tagsValue != nil {
				tags, err = common.ToStringList(tagsValue)
				if err != nil {
					return starlark.None, err
				}
			}

			pkg := common.NewPackage(name, aliases, raw, tags)

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
func (parser *packageCollection) AttrNames() []string {
	return []string{"add_package"}
}

// Tag implements BuildSource.
func (parser *packageCollection) Tag() string {
	return strings.Join([]string{parser.Filename, parser.Parser, parser.Install}, "_")
}

func (parser *packageCollection) load(ctx *buildContext) error {
	var records []starlark.Value

	start := time.Now()

	// Build all the package sources.
	// This can happen in parallel.
	for _, source := range parser.Sources {
		built, err := ctx.BuildChild(source)
		if err != nil {
			return err
		}

		builtFile, err := built.Default()
		if err != nil {
			return err
		}

		fh, err := builtFile.Open()
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

	slog.Debug("built all package sources", "took", time.Since(start))
	start = time.Now()

	parserCallback, err := ctx.builder.database.getBuilder(parser.Filename, parser.Parser)
	if err != nil {
		return fmt.Errorf("failed to GetBuilder in PackageCollection.Load: %s", err)
	}

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

			child := ctx.childContext(parser, nil, "")

			thread := ctx.builder.database.newThread(parser.Filename)

			_, err := starlark.Call(thread, parserCallback, starlark.Tuple{child, parser, starlark.NewList(records)}, []starlark.Tuple{})
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
		slog.Debug("loaded all packages", "count", len(records), "took", time.Since(start))

		return nil
	}
}

//

func (parser *packageCollection) Query(query common.PackageQuery) ([]*common.Package, error) {
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
	} else if len(query.Tags) > 0 {
		for _, pkg := range parser.RawPackages {
			if pkg.Matches(query) {
				directs = append(directs, pkg)
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

func (parser *packageCollection) InstallerFor(c common.BuildContext1, pkg *common.Package, tags common.TagList) (*common.Installer, error) {
	ctx, ok := c.(*buildContext)
	if !ok {
		return nil, fmt.Errorf("could not convert %s to buildContext", c.Type())
	}

	getInstall, err := ctx.builder.database.getBuilder(parser.Filename, parser.Install)
	if err != nil {
		return nil, fmt.Errorf("failed to get builder in InstallerFor: %s", err)
	}

	ret, err := starlark.Call(ctx.builder.database.newThread(parser.Filename), getInstall, starlark.Tuple{pkg, tags}, []starlark.Tuple{})
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

func (def *packageCollection) String() string { return def.Tag() }
func (*packageCollection) Type() string       { return "PackageCollection" }
func (*packageCollection) Hash() (uint32, error) {
	return 0, fmt.Errorf("PackageCollection is not hashable")
}
func (*packageCollection) Truth() starlark.Bool { return starlark.True }
func (*packageCollection) Freeze()              {}

var (
	_ starlark.Value          = &packageCollection{}
	_ starlark.HasAttrs       = &packageCollection{}
	_ common.BuildDefinition1 = &packageCollection{}
)

func newPackageCollection(
	filename string,
	parser string,
	install string,
	sources []common.BuildDefinition1,
) (common.PackageCollection, error) {
	return &packageCollection{
		Filename:    filename,
		Parser:      parser,
		Install:     install,
		Sources:     sources,
		Packages:    make(map[string][]*common.Package),
		RawPackages: make(map[string]*common.Package),
	}, nil
}
