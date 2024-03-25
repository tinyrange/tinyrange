package db

import (
	"fmt"
	"log/slog"

	"github.com/tinyrange/pkg2/core"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type PackageName struct {
	Distribution string
	Namespace    string
	Name         string
	Version      string
	Architecture string
}

func (name PackageName) Matches(query PackageName) bool {
	if query.Distribution != "" {
		if query.Distribution != name.Distribution {
			return false
		}
	}

	if query.Architecture != "" {
		if query.Architecture != name.Architecture {
			return false
		}
	}

	if query.Name != "" {
		if query.Name != name.Name {
			return false
		}
	}

	if query.Version != "" {
		if query.Version != name.Version {
			return false
		}
	}

	return true
}

func (name PackageName) String() string {
	return fmt.Sprintf("%s/%s@%s:%s", name.Distribution, name.Name, name.Version, name.Architecture)
}
func (PackageName) Type() string          { return "PackageName" }
func (PackageName) Hash() (uint32, error) { return 0, fmt.Errorf("PackageName is not hashable") }
func (PackageName) Truth() starlark.Bool  { return starlark.True }
func (PackageName) Freeze()               {}

var (
	_ starlark.Value = PackageName{}
)

type Package struct {
	Name          PackageName
	Description   string
	License       string
	Size          int
	InstalledSize int
	DownloadUrls  []string
	Metadata      map[string]string
	Depends       [][]PackageName
	Aliases       []PackageName
}

func (pkg *Package) Matches(query PackageName) bool {
	ok := pkg.Name.Matches(query)
	if ok {
		return true
	}

	for _, alias := range pkg.Aliases {
		if ok = alias.Matches(query); ok {
			return true
		}
	}

	return false
}

// Attr implements starlark.HasAttrs.
func (pkg *Package) Attr(name string) (starlark.Value, error) {
	if name == "set_description" {
		return starlark.NewBuiltin("Package.set_description", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				description string
			)

			if err := starlark.UnpackArgs("Package.set_description", args, kwargs,
				"description", &description,
			); err != nil {
				return starlark.None, err
			}

			pkg.Description = description

			return starlark.None, nil
		}), nil
	} else if name == "set_license" {
		return starlark.NewBuiltin("Package.set_license", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				license string
			)

			if err := starlark.UnpackArgs("Package.set_license", args, kwargs,
				"license", &license,
			); err != nil {
				return starlark.None, err
			}

			pkg.License = license

			return starlark.None, nil
		}), nil
	} else if name == "set_size" {
		return starlark.NewBuiltin("Package.set_size", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				size int
			)

			if err := starlark.UnpackArgs("Package.set_size", args, kwargs,
				"size", &size,
			); err != nil {
				return starlark.None, err
			}

			pkg.Size = size

			return starlark.None, nil
		}), nil
	} else if name == "set_installed_size" {
		return starlark.NewBuiltin("Package.set_installed_size", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				size int
			)

			if err := starlark.UnpackArgs("Package.set_installed_size", args, kwargs,
				"size", &size,
			); err != nil {
				return starlark.None, err
			}

			pkg.InstalledSize = size

			return starlark.None, nil
		}), nil
	} else if name == "add_source" {
		return starlark.NewBuiltin("Package.add_source", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				url string
			)

			if err := starlark.UnpackArgs("Package.add_source", args, kwargs,
				"url", &url,
			); err != nil {
				return starlark.None, err
			}

			pkg.DownloadUrls = append(pkg.DownloadUrls, url)

			return starlark.None, nil
		}), nil
	} else if name == "add_metadata" {
		return starlark.NewBuiltin("Package.add_metadata", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				key   string
				value string
			)

			if err := starlark.UnpackArgs("Package.add_metadata", args, kwargs,
				"key", &key,
				"value", &value,
			); err != nil {
				return starlark.None, err
			}

			if value != "" {
				pkg.Metadata[key] = value
			}

			return starlark.None, nil
		}), nil
	} else if name == "add_dependency" {
		return starlark.NewBuiltin("Package.add_dependency", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name starlark.Value
				kind string
			)

			if err := starlark.UnpackArgs("Package.add_alias", args, kwargs,
				"name", &name,
				"kind?", &kind,
			); err != nil {
				return starlark.None, err
			}

			if pkgName, ok := name.(PackageName); ok {
				pkg.Depends = append(pkg.Depends, []PackageName{pkgName})

				return starlark.None, nil
			} else if names, ok := name.(*starlark.List); ok {
				var options []PackageName

				var err error

				names.Elements(func(v starlark.Value) bool {
					pkgName, ok := v.(PackageName)
					if ok {
						options = append(options, pkgName)
						return true
					} else {
						err = fmt.Errorf("expected PackageName got %s", name.Type())
						return false
					}
				})
				if err != nil {
					return starlark.None, err
				}

				pkg.Depends = append(pkg.Depends, options)

				return starlark.None, nil
			} else {
				return starlark.None, fmt.Errorf("unhandled argument type: %T", name)
			}
		}), nil
	} else if name == "add_alias" {
		return starlark.NewBuiltin("Package.add_alias", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name PackageName
				kind string
			)

			if err := starlark.UnpackArgs("Package.add_alias", args, kwargs,
				"name", &name,
				"kind?", &kind,
			); err != nil {
				return starlark.None, err
			}

			pkg.Aliases = append(pkg.Aliases, name)

			return starlark.None, nil
		}), nil
	} else if name == "name" {
		return starlark.String(pkg.Name.Name), nil
	} else if name == "version" {
		return starlark.String(pkg.Name.Version), nil
	} else if name == "arch" {
		return starlark.String(pkg.Name.Architecture), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (*Package) AttrNames() []string {
	return []string{
		"set_description",
		"set_license",
		"set_size",
		"set_installed_size",
		"add_source",
		"add_metadata",
		"add_dependency",
		"add_alias",
		"name",
		"version",
		"arch",
	}
}

func (*Package) String() string        { return "Package" }
func (*Package) Type() string          { return "Package" }
func (*Package) Hash() (uint32, error) { return 0, fmt.Errorf("Package is not hashable") }
func (*Package) Truth() starlark.Bool  { return starlark.True }
func (*Package) Freeze()               {}

var (
	_ starlark.Value    = &Package{}
	_ starlark.HasAttrs = &Package{}
)

type RepositoryFetcher struct {
	db     *PackageDatabase
	Distro string
	Func   *starlark.Function
	Args   starlark.Tuple
}

// Attr implements starlark.HasAttrs.
func (r *RepositoryFetcher) Attr(name string) (starlark.Value, error) {
	if name == "add_package" {
		return starlark.NewBuiltin("Repo.add_package", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name PackageName
			)

			if err := starlark.UnpackArgs("Repo.add_package", args, kwargs,
				"name", &name,
			); err != nil {
				return starlark.None, err
			}

			return r.db.addPackage(name), nil
		}), nil
	} else if name == "name" {
		return starlark.NewBuiltin("Repo.name", func(
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

			if err := starlark.UnpackArgs("Repo.name", args, kwargs,
				"namespace?", &namespace,
				"name", &name,
				"version?", &version,
				"distro?", &distro,
				"architecture?", &architecture,
			); err != nil {
				return starlark.None, err
			}

			if distro == "" {
				distro = r.Distro
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
func (*RepositoryFetcher) AttrNames() []string {
	return []string{"add_package"}
}

func (*RepositoryFetcher) String() string { return "RepositoryFetcher" }
func (*RepositoryFetcher) Type() string   { return "RepositoryFetcher" }
func (*RepositoryFetcher) Hash() (uint32, error) {
	return 0, fmt.Errorf("RepositoryFetcher is not hashable")
}
func (*RepositoryFetcher) Truth() starlark.Bool { return starlark.True }
func (*RepositoryFetcher) Freeze()              {}

var (
	_ starlark.Value    = &RepositoryFetcher{}
	_ starlark.HasAttrs = &RepositoryFetcher{}
)

type PackageDatabase struct {
	Eif      *core.EnvironmentInterface
	Fetchers []*RepositoryFetcher
	Packages []*Package
}

func (db *PackageDatabase) addPackage(name PackageName) *Package {
	pkg := &Package{
		Name:     name,
		Metadata: make(map[string]string),
	}
	db.Packages = append(db.Packages, pkg)
	return pkg
}

func (db *PackageDatabase) addRepositoryFetcher(distro string, f *starlark.Function, args starlark.Tuple) error {
	db.Fetchers = append(db.Fetchers, &RepositoryFetcher{
		db:     db,
		Distro: distro,
		Func:   f,
		Args:   args,
	})

	return nil
}

func (db *PackageDatabase) LoadScript(filename string) error {
	thread := &starlark.Thread{}

	globals := starlark.StringDict{
		"fetch_http": starlark.NewBuiltin("fetch_http", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				url string
			)

			if err := starlark.UnpackArgs("fetch_http", args, kwargs,
				"url", &url,
			); err != nil {
				return starlark.None, err
			}

			f, err := db.Eif.HttpGetReader(url)
			if err != nil {
				return starlark.None, err
			}

			return &StarFile{f: f, name: url}, nil
		}),
		"fetch_repo": starlark.NewBuiltin("fetch_repo", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				distro string
				f      *starlark.Function
				fArgs  starlark.Tuple
			)

			if err := starlark.UnpackArgs("fetch_repo", args, kwargs,
				"f", &f,
				"fArgs", &fArgs,
				"distro?", &distro,
			); err != nil {
				return starlark.None, err
			}

			if err := db.addRepositoryFetcher(distro, f, fArgs); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}),
		"error": starlark.NewBuiltin("error", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				message string
			)

			if err := starlark.UnpackArgs("error", args, kwargs,
				"message", &message,
			); err != nil {
				return starlark.None, err
			}

			return starlark.None, fmt.Errorf("%s", message)
		}),
		"json": starlarkjson.Module,
	}

	_, err := starlark.ExecFileOptions(&syntax.FileOptions{
		TopLevelControl: true,
		Recursion:       true,
	}, thread, filename, nil, globals)
	if err != nil {
		return err
	}

	return nil
}

func (db *PackageDatabase) FetchAll() error {
	for _, fetcher := range db.Fetchers {
		thread := &starlark.Thread{}

		_, err := starlark.Call(thread, fetcher.Func,
			append(starlark.Tuple{fetcher}, fetcher.Args...),
			[]starlark.Tuple{},
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func (db *PackageDatabase) Search(query PackageName, maxResults int) ([]*Package, error) {
	var ret []*Package

	slog.Info("search", "query", query)

	for _, pkg := range db.Packages {
		if pkg.Matches(query) {
			ret = append(ret, pkg)
			if maxResults != 0 && len(ret) >= maxResults {
				break
			}
		}
	}

	return ret, nil
}
