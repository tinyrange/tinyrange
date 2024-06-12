package database

import (
	"fmt"

	"github.com/tinyrange/pkg2/v2/common"
	"go.starlark.net/starlark"
)

type InstallationPlan struct {
	Packages   []*common.Package
	Directives []common.Directive
}

func EmitDockerfile(plan *InstallationPlan) (string, error) {
	ret := ""

	for _, directive := range plan.Directives {
		switch directive := directive.(type) {
		case common.DirectiveBaseImage:
			ret += fmt.Sprintf("FROM %s\n", string(directive))
		case common.DirectiveRunCommand:
			ret += fmt.Sprintf("RUN %s\n", string(directive))
		default:
			return "", fmt.Errorf("directive %T not handled for docker", directive)
		}
	}

	return ret, nil
}

type ContainerBuilder struct {
	Name           string
	DisplayName    string
	BaseDirectives []common.Directive
	Packages       *PackageCollection

	loaded bool
}

func (builder *ContainerBuilder) Loaded() bool {
	return builder.loaded
}

func (builder *ContainerBuilder) Load(db *PackageDatabase) error {
	if builder.Loaded() {
		return nil
	}

	if err := builder.Packages.Load(db); err != nil {
		return err
	}

	builder.loaded = true

	return nil
}

func (builder *ContainerBuilder) Plan(packages []common.PackageQuery) (*InstallationPlan, error) {
	plan := &InstallationPlan{}

	plan.Directives = append(plan.Directives, builder.BaseDirectives...)

	for _, pkg := range packages {
		results, err := builder.Packages.Query(pkg)
		if err != nil {
			return nil, err
		}

		if len(results) == 0 {
			return nil, fmt.Errorf("could not find package for query: %s", pkg)
		}

		result := results[0]

		plan.Directives = append(plan.Directives, result.Directives...)
		plan.Packages = append(plan.Packages, result)
	}

	return plan, nil
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
	_ starlark.Value = &ContainerBuilder{}
)

func NewContainerBuilder(name string, displayName string, baseDirectives []common.Directive, packages *PackageCollection) (*ContainerBuilder, error) {
	return &ContainerBuilder{
		Name:           name,
		BaseDirectives: baseDirectives,
		Packages:       packages,
	}, nil
}
