package main

import "testing"

type packageMetadata struct {
	id      PackageID
	version string
}

type testSource struct {
	versions     map[string][]packageMetadata
	dependencies map[PackageID][]PackageQuery
	directives   map[PackageID][]Directive
}

func (s *testSource) FindPackages(planner Planner, query PackageQuery) ([]PackageID, error) {
	var ids []PackageID
	for _, meta := range s.versions[query.Name] {
		if query.Condition.SatisfiedByVersion(planner, meta.version) {
			ids = append(ids, meta.id)
		}
	}
	return ids, nil
}

func (s *testSource) GetPackageDependencies(id PackageID) ([]PackageQuery, error) {
	return s.dependencies[id], nil
}

func (s *testSource) GetPackageDirectives(id PackageID) ([]Directive, error) {
	return s.directives[id], nil
}

var (
	_ PackageSource = &testSource{}
)

type testDirective struct {
	id PackageID
}

func (d *testDirective) Execute() error {
	return nil
}

type AnyVersion struct{}

func (a AnyVersion) SatisfiedByVersion(planner Planner, v string) bool {
	return true
}

func TestBasic(t *testing.T) {
	p := NewPlanner(nil)

	source := &testSource{
		versions: map[string][]packageMetadata{
			"foo": {
				{PackageID("foo@1.0.0"), "1.0.0"},
				{PackageID("foo@2.0.0"), "2.0.0"},
			},
			"bar": {
				{PackageID("bar@1.0.0"), "1.0.0"},
			},
		},
		dependencies: map[PackageID][]PackageQuery{
			"foo@1.0.0": {
				{"bar", AnyVersion{}},
			},
			"foo@2.0.0": {
				{"bar", AnyVersion{}},
			},
			"bar@1.0.0": {},
		},
		directives: map[PackageID][]Directive{
			"bar@1.0.0": {
				&testDirective{"bar@1.0.0"},
			},
		},
	}
	if err := p.AddSource(source); err != nil {
		t.Fatal(err)
	}

	if err := p.AddPackages([]PackageQuery{
		{"foo", AnyVersion{}},
	}); err != nil {
		t.Fatal(err)
	}

	directives, err := p.Plan()
	if err != nil {
		t.Fatal(err)
	}

	if len(directives) != 2 {
		t.Fatalf("expected 2 directives, got %d", len(directives))
	}
}
