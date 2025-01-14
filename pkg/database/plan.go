package database

import (
	"fmt"
	"io"

	"github.com/fatih/color"
	"github.com/tinyrange/tinyrange/pkg/common"
	"go.starlark.net/starlark"
)

var (
	colorRed   = color.New(color.FgRed)
	colorGreen = color.New(color.FgGreen)
	colorFaint = color.New(color.Faint)
)

type installOption struct {
	pkg     *common.Package
	install *common.Installer
}

type installationTree struct {
	Query        common.PackageQuery
	Package      *common.Package
	Installer    *common.Installer
	Options      []installOption
	Error        error
	Dependencies []*installationTree
}

func (t *installationTree) writeTree(output io.Writer, prefix string) error {
	if t.Error != nil {
		colorRed.Fprintf(output, "%s- [%s]\n", prefix, t.Error)
		return nil
	}

	if t.Installer == nil {
		if !common.IsVerbose() {
			return nil
		}

		if t.Package == nil {
			colorFaint.Fprintf(output, "%s- %s (installed)\n", prefix, t.Query)
		} else {
			colorFaint.Fprintf(output, "%s- %s (installed %s)\n", prefix, t.Query, t.Package.Name)
		}
	} else if t.Query.Equals(t.Package.Name) {
		colorGreen.Fprintf(output, "%s- %s\n", prefix, t.Query)
	} else {
		if t.Package == nil {
			colorGreen.Fprintf(output, "%s- (%s)\n", prefix, t.Query)
		} else {
			colorGreen.Fprintf(output, "%s- %s(%s)\n", prefix, t.Package.Name, t.Query)
		}
	}

	for _, depend := range t.Dependencies {
		if err := depend.writeTree(output, prefix+"  "); err != nil {
			return err
		}
	}

	return nil
}

func (t *installationTree) Packages() []*common.Package {
	var ret []*common.Package

	ret = append(ret, t.Package)

	for _, depend := range t.Dependencies {
		pkgs := depend.Packages()

		ret = append(ret, pkgs...)
	}

	return ret
}

type installInfo struct {
	version string
	pkg     *common.Package
}

type installationPlan struct {
	trees          []*installationTree
	directives     []common.Directive
	baseDirectives []common.Directive
	tags           common.TagList
	options        common.PlanOptions

	installedNames map[string]*installInfo // map of names and versions.
}

// WriteTree implements common.InstallationPlan.
func (plan *installationPlan) WriteTree(output io.Writer) error {
	for _, tree := range plan.trees {
		if err := tree.writeTree(output, ""); err != nil {
			return err
		}
	}

	return nil
}

// SetDirectives implements common.InstallationPlan.
func (plan *installationPlan) SetDirectives(directives []common.Directive) {
	plan.directives = directives
}

// Directives implements common.InstallationPlan.
func (plan *installationPlan) Directives() []common.Directive {
	return plan.directives
}

// Attr implements starlark.HasAttrs.
func (plan *installationPlan) Attr(name string) (starlark.Value, error) {
	if name == "packages" {
		var elems []starlark.Value

		for _, tree := range plan.trees {
			pkgs := tree.Packages()
			for _, pkg := range pkgs {
				elems = append(elems, pkg)
			}
		}

		return starlark.NewList(elems), nil
	} else if name == "directives" {
		var elems []starlark.Value

		for _, directive := range plan.directives {
			if val, ok := directive.(starlark.Value); ok {
				elems = append(elems, val)
			} else {
				elems = append(elems, &common.StarDirective{Directive: directive})
			}
		}

		return starlark.NewList(elems), nil
	} else if name == "base_directives" {
		var elems []starlark.Value

		for _, directive := range plan.baseDirectives {
			if val, ok := directive.(starlark.Value); ok {
				elems = append(elems, val)
			} else {
				elems = append(elems, &common.StarDirective{Directive: directive})
			}
		}

		return starlark.NewList(elems), nil
	} else if name == "tags" {
		return plan.tags, nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (plan *installationPlan) AttrNames() []string {
	return []string{"packages", "directives", "base_directives", "tags"}
}

func (plan *installationPlan) checkName(name common.PackageName) (*common.Package, bool) {
	// slog.Info("checkName", "name", name)

	installed, ok := plan.installedNames[name.Name]
	if !ok {
		return nil, false
	}

	return installed.pkg, true
}

func (plan *installationPlan) addName(name common.PackageName, pkg *common.Package) {
	// slog.Info("addName", "name", name, "pkg", pkg)

	plan.installedNames[name.Name] = &installInfo{
		version: name.Version,
		pkg:     pkg,
	}
}

func (plan *installationPlan) addInternal(
	ctx common.BuildContext1,
	builder common.ContainerBuilder,
	query common.PackageQuery,
	options []installOption,
	option installOption,
	isDefault bool,
) (ret *installationTree) {
	ret = &installationTree{Query: query}

	ret.Options = options

	// Check to see if this package is already installed.
	if pkg, ok := plan.checkName(option.pkg.Name); ok {
		ret.Package = pkg
		// slog.Info("already installed")
		return ret // already installed.
	}
	// for _, alias := range option.pkg.Aliases {
	// 	if pkg, ok := plan.checkName(alias); ok {
	// 		ret.Package = pkg
	// 		slog.Info("already installed")
	// 		return ret // already installed.
	// 	}
	// }

	// Confirm that we are using this package.
	// From here the method must return or fail entirely.
	ret.Package = option.pkg
	ret.Installer = option.install

	// slog.Info("selected", "pkg", ret.Package, "install", ret.Installer)

	// Add the package
	plan.addName(option.pkg.Name, option.pkg)
	for _, alias := range option.pkg.Aliases {
		plan.addName(alias, option.pkg)
	}

	// For each dependency add it.
	for _, depend := range option.install.Dependencies {
		child := plan.add(ctx, builder, depend, isDefault)
		if child.Error != nil && !plan.options.Debug {
			ret.Error = fmt.Errorf("error adding dependency for package %s: %s", query, child.Error)
			return
		}

		ret.Dependencies = append(ret.Dependencies, child)
	}

	// Add the directives for the installer.
	// This is after the dependencies are added first.
	if isDefault {
		plan.baseDirectives = append(plan.baseDirectives, option.install.Directives...)
	} else {
		plan.directives = append(plan.directives, option.install.Directives...)
	}

	return
}

func (plan *installationPlan) add(ctx common.BuildContext1, builder common.ContainerBuilder, query common.PackageQuery, isDefault bool) (ret *installationTree) {
	ret = &installationTree{Query: query}

	// Query for any packages matching the query.
	results, err := builder.Packages().Query(query)
	if err != nil {
		ret.Error = err
		return
	}

	// Early out if we can't find a package matching the query.
	if len(results) == 0 {
		ret.Error = fmt.Errorf("could not find package for query: %s", query)
		return
	}

	// Collect all possible options.
	var options []installOption

	for _, result := range results {
		installer, err := builder.Packages().InstallerFor(ctx, result, plan.tags)
		if err != nil {
			ret.Error = fmt.Errorf("failed to get installer for %s", result.Name)
			return
		}

		options = append(options, installOption{
			pkg:     result,
			install: installer,
		})
	}

	ret.Options = options

	// Raise a error if we can't find a matching installer.
	if len(options) == 0 {
		ret.Error = fmt.Errorf("could not find installer for package: %s", query)
		return
	}

	if len(query.Tags) > 0 {
		for _, option := range options {
			child := plan.addInternal(ctx, builder, query, options, option, isDefault)
			if child.Error != nil && !plan.options.Debug {
				ret.Error = fmt.Errorf("error adding dependency for query %s: %s", query, child.Error)
				return
			}

			ret.Dependencies = append(ret.Dependencies, child)
		}

		return ret
	} else {
		option := options[0]
		return plan.addInternal(ctx, builder, query, options, option, isDefault)
	}
}

func (plan *installationPlan) Add(ctx common.BuildContext1, builder common.ContainerBuilder, query common.PackageQuery, isDefault bool) error {
	tree := plan.add(ctx, builder, query, isDefault)
	if tree.Error != nil && !plan.options.Debug {
		return tree.Error
	}

	plan.trees = append(plan.trees, tree)

	return nil
}

func (*installationPlan) String() string { return "InstallationPlan" }
func (*installationPlan) Type() string   { return "InstallationPlan" }
func (*installationPlan) Hash() (uint32, error) {
	return 0, fmt.Errorf("InstallationPlan is not hashable")
}
func (*installationPlan) Truth() starlark.Bool { return starlark.True }
func (*installationPlan) Freeze()              {}

var (
	_ starlark.Value          = &installationPlan{}
	_ starlark.HasAttrs       = &installationPlan{}
	_ common.InstallationPlan = &installationPlan{}
)

func newInstallationPlan(tags common.TagList, opts common.PlanOptions) common.InstallationPlan {
	return &installationPlan{
		installedNames: make(map[string]*installInfo),
		tags:           tags,
		options:        opts,
	}
}
