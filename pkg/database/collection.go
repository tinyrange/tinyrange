package database

import (
	"fmt"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/record"
	"go.starlark.net/starlark"
)

type PackageCollection struct {
	Name               []string
	Parser             starlark.Callable
	Sources            []common.BuildDefinition
	AdditionalPackages []*common.Package

	Packages map[string]*common.Package
}

// Tag implements BuildSource.
func (parser *PackageCollection) Tag() string {
	return strings.Join(parser.Name, "_")
}

func (parser *PackageCollection) Load(db *PackageDatabase) error {
	var records []starlark.Value

	ctx := db.NewBuildContext(parser)

	// Build all the package sources.
	// This can happen in parallel.
	for _, source := range parser.Sources {
		built, err := db.Build(ctx, source, common.BuildOptions{})
		if err != nil {
			return err
		}

		sourceRecords, err := record.ReadRecordsFromFile(built)
		if err != nil {
			return err
		}

		records = append(records, sourceRecords...)
	}

	// For each record in the list call the parser to parse the record into a package.
	// This can also happen in parallel,
	for _, record := range records {
		child := ctx.ChildContext(parser, nil, "")

		_, err := child.Call(parser.Parser, record)
		if err != nil {
			return err
		}

		for _, pkg := range child.Packages() {
			parser.Packages[pkg.Name.Key()] = pkg
		}
	}

	for _, pkg := range parser.AdditionalPackages {
		parser.Packages[pkg.Name.Key()] = pkg
	}

	return nil
}

func (parser *PackageCollection) Query(query common.PackageQuery) ([]*common.Package, error) {
	var ret []*common.Package

	for _, pkg := range parser.Packages {
		if pkg.Matches(query) {
			ret = append(ret, pkg)
		}
	}

	return ret, nil
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
	_ common.BuildSource = &PackageCollection{}
)

func NewPackageCollection(name string, f starlark.Callable, sources []common.BuildDefinition, additionalPackages []*common.Package) (*PackageCollection, error) {
	return &PackageCollection{
		Name:               []string{name, f.Name()},
		Parser:             f,
		Sources:            sources,
		AdditionalPackages: additionalPackages,
		Packages:           make(map[string]*common.Package),
	}, nil
}
