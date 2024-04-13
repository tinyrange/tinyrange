package db

import (
	"fmt"

	"go.starlark.net/starlark"
)

type SearchProvider struct {
	db           *PackageDatabase
	Distribution string
	Func         *starlark.Function
	Args         starlark.Tuple

	Packages []*Package
}

func (s *SearchProvider) addPackage(name PackageName) starlark.Value {
	pkg := NewPackage()
	pkg.Name = name
	s.Packages = append(s.Packages, pkg)
	return pkg
}

// Attr implements starlark.HasAttrs.
func (s *SearchProvider) Attr(name string) (starlark.Value, error) {
	if name == "add_package" {
		return starlark.NewBuiltin("Search.add_package", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name PackageName
			)

			if err := starlark.UnpackArgs("Search.add_package", args, kwargs,
				"name", &name,
			); err != nil {
				return starlark.None, err
			}

			return s.addPackage(name), nil
		}), nil
	} else if name == "name" {
		return starlark.NewBuiltin("Search.name", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				namespace    string
				name         string
				version      string
				distro       string
				architecture string
			)

			if err := starlark.UnpackArgs("Search.name", args, kwargs,
				"namespace?", &namespace,
				"name", &name,
				"version?", &version,
				"distro?", &distro,
				"architecture?", &architecture,
			); err != nil {
				return starlark.None, err
			}

			if distro == "" {
				distro = s.Distribution
			}

			return PackageName{
				Distribution: distro,
				Namespace:    namespace,
				Name:         name,
				Version:      version,
				Architecture: architecture,
			}, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (s *SearchProvider) AttrNames() []string {
	return []string{}
}

func (*SearchProvider) String() string { return "SearchProvider" }
func (*SearchProvider) Type() string   { return "SearchProvider" }
func (*SearchProvider) Hash() (uint32, error) {
	return 0, fmt.Errorf("ScriptFetcher is not hashable")
}
func (*SearchProvider) Truth() starlark.Bool { return starlark.True }
func (*SearchProvider) Freeze()              {}

func (s *SearchProvider) searchExisting(name PackageName, maxResults int) ([]*Package, error) {
	var ret []*Package

	for _, pkg := range s.Packages {
		if pkg.Matches(name) {
			ret = append(ret, pkg)
			if maxResults != 0 && len(ret) >= maxResults {
				break
			}
		}
	}

	return ret, nil
}

func (s *SearchProvider) Search(name PackageName, maxResults int) ([]*Package, error) {
	results, err := s.searchExisting(name, maxResults)
	if err != nil {
		return nil, err
	}
	if len(results) != 0 {
		return results, nil
	}

	// Call the user provided function to do the search.
	thread := &starlark.Thread{}

	_, err = starlark.Call(thread, s.Func, starlark.Tuple{s, name}, []starlark.Tuple{})
	if err != nil {
		return nil, err
	}

	return s.searchExisting(name, maxResults)
}

var (
	_ starlark.Value    = &SearchProvider{}
	_ starlark.HasAttrs = &SearchProvider{}
)
