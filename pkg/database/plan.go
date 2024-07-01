package database

import (
	"fmt"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"go.starlark.net/starlark"
)

type InstallationPlan struct {
	Packages   []*common.Package
	Directives []common.Directive
	Tags       common.TagList

	installedNames map[string]string // map of names and versions.
}

// Attr implements starlark.HasAttrs.
func (plan *InstallationPlan) Attr(name string) (starlark.Value, error) {
	if name == "packages" {
		var elems []starlark.Value

		for _, pkg := range plan.Packages {
			elems = append(elems, pkg)
		}

		return starlark.NewList(elems), nil
	} else if name == "directives" {
		var elems []starlark.Value

		for _, directive := range plan.Directives {
			if val, ok := directive.(starlark.Value); ok {
				elems = append(elems, val)
			} else {
				elems = append(elems, &common.StarDirective{Directive: directive})
			}
		}

		return starlark.NewList(elems), nil
	} else if name == "tags" {
		return plan.Tags, nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (plan *InstallationPlan) AttrNames() []string {
	return []string{"packages", "directives", "tags"}
}

func (plan *InstallationPlan) checkName(name common.PackageName) bool {
	_, ok := plan.installedNames[name.Name]
	return ok
}

func (plan *InstallationPlan) addName(name common.PackageName) {
	plan.installedNames[name.Name] = name.Version
}

func (plan *InstallationPlan) Add(builder *ContainerBuilder, query common.PackageQuery, tags common.TagList) error {
	results, err := builder.Packages.Query(query)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		return fmt.Errorf("could not find package for query: %s", query)
	}

	var options []struct {
		pkg     *common.Package
		install *common.Installer
	}

	for _, result := range results {
		for _, installer := range result.Installers {
			if installer.Tags.Matches(tags) {
				options = append(options, struct {
					pkg     *common.Package
					install *common.Installer
				}{
					pkg:     result,
					install: installer,
				})
			}
		}
	}

	if len(options) == 0 {
		return fmt.Errorf("could not find installer for package: %s", query)
	}

	option := options[0]

	// Check to see if this package is already installed.
	if plan.checkName(option.pkg.Name) {
		return nil // already installed.
	}
	for _, alias := range option.pkg.Aliases {
		if plan.checkName(alias) {
			return nil // already installed.
		}
	}

	// Add the package
	plan.addName(option.pkg.Name)
	for _, alias := range option.pkg.Aliases {
		plan.addName(alias)
	}

	plan.Packages = append(plan.Packages, option.pkg)

	for _, depend := range option.install.Dependencies {
		if err := plan.Add(builder, depend, tags); err != nil {
			return fmt.Errorf("error adding dependency for package %s: %s", query, err)
		}
	}

	plan.Directives = append(plan.Directives, option.install.Directives...)

	return nil
}

func (*InstallationPlan) String() string { return "InstallationPlan" }
func (*InstallationPlan) Type() string   { return "InstallationPlan" }
func (*InstallationPlan) Hash() (uint32, error) {
	return 0, fmt.Errorf("InstallationPlan is not hashable")
}
func (*InstallationPlan) Truth() starlark.Bool { return starlark.True }
func (*InstallationPlan) Freeze()              {}

var (
	_ starlark.Value    = &InstallationPlan{}
	_ starlark.HasAttrs = &InstallationPlan{}
)

func EmitDockerfile(plan *InstallationPlan) (string, error) {
	ret := ""

	for _, directive := range plan.Directives {
		switch directive := directive.(type) {
		case *builder.FetchOciImageDefinition:
			ret += fmt.Sprintf("FROM %s\n", directive.FromDirective())
		case common.DirectiveRunCommand:
			ret += fmt.Sprintf("RUN %s\n", string(directive))
		default:
			return "", fmt.Errorf("directive %T not handled for docker", directive)
		}
	}

	return ret, nil
}
