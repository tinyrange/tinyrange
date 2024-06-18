package database

import (
	"fmt"

	"github.com/tinyrange/pkg2/v2/common"
	"go.starlark.net/starlark"
)

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

func (builder *ContainerBuilder) Search(pkg common.PackageQuery) ([]*common.Package, error) {
	return builder.Packages.Query(pkg)
}

func (builder *ContainerBuilder) Get(key string) (*common.Package, bool) {
	pkg, ok := builder.Packages.Packages[key]
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
	_ starlark.Value = &ContainerBuilder{}
)

func NewContainerBuilder(name string, displayName string, baseDirectives []common.Directive, packages *PackageCollection) (*ContainerBuilder, error) {
	return &ContainerBuilder{
		Name:           name,
		DisplayName:    displayName,
		BaseDirectives: baseDirectives,
		Packages:       packages,
	}, nil
}
