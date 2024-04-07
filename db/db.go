package db

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	xj "github.com/basgys/goxml2json"
	"github.com/icza/dyno"
	"github.com/minio/sha256-simd"
	"github.com/tinyrange/pkg2/core"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"gopkg.in/yaml.v3"
	"howett.net/plist"
)

func getSha256(val []byte) string {
	sum := sha256.Sum256(val)
	return hex.EncodeToString(sum[:])
}

type RepositoryFetcher struct {
	db       *PackageDatabase
	Packages []*Package
	Distro   string
	Func     *starlark.Function
	Args     starlark.Tuple
}

func (r *RepositoryFetcher) Key() (string, error) {
	var tokens []string

	tokens = append(tokens, r.Func.Name())

	for _, arg := range r.Args {
		str, ok := starlark.AsString(arg)
		if !ok {
			str = arg.String()
		}

		tokens = append(tokens, str)
	}

	return getSha256([]byte(strings.Join(tokens, "_"))), nil
}

func (r *RepositoryFetcher) addPackage(name PackageName) starlark.Value {
	pkg := NewPackage()
	pkg.Name = name
	r.Packages = append(r.Packages, pkg)
	return pkg
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

			return r.addPackage(name), nil
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

type ScriptFetcher struct {
	db   *PackageDatabase
	Name string
	Func *starlark.Function
	Args starlark.Tuple
}

// Attr implements starlark.HasAttrs.
func (s *ScriptFetcher) Attr(name string) (starlark.Value, error) {
	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (s *ScriptFetcher) AttrNames() []string {
	return []string{}
}

func (*ScriptFetcher) String() string { return "ScriptFetcher" }
func (*ScriptFetcher) Type() string   { return "ScriptFetcher" }
func (*ScriptFetcher) Hash() (uint32, error) {
	return 0, fmt.Errorf("ScriptFetcher is not hashable")
}
func (*ScriptFetcher) Truth() starlark.Bool { return starlark.True }
func (*ScriptFetcher) Freeze()              {}

var (
	_ starlark.Value    = &ScriptFetcher{}
	_ starlark.HasAttrs = &ScriptFetcher{}
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

type PackageDatabase struct {
	Eif             *core.EnvironmentInterface
	Fetchers        []*RepositoryFetcher
	ScriptFetchers  []*ScriptFetcher
	SearchProviders []*SearchProvider
	Packages        []*Package
	PackageMap      map[string]*Package
	AllowLocal      bool
	ForceRefresh    bool
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

func (db *PackageDatabase) addScriptFetcher(name string, f *starlark.Function, args starlark.Tuple) error {
	db.ScriptFetchers = append(db.ScriptFetchers, &ScriptFetcher{
		db:   db,
		Name: name,
		Func: f,
		Args: args,
	})

	return nil
}

func (db *PackageDatabase) addSearchProvider(distro string, f *starlark.Function, args starlark.Tuple) error {
	db.SearchProviders = append(db.SearchProviders, &SearchProvider{
		db:           db,
		Distribution: distro,
		Func:         f,
		Args:         args,
	})

	return nil
}

func (db *PackageDatabase) getGlobals(name string) (starlark.StringDict, error) {
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
			if err == core.ErrNotFound {
				return starlark.None, nil
			} else if err != nil {
				return starlark.None, err
			}

			return &StarFile{f: f, name: url}, nil
		}),
		"fetch_git": starlark.NewBuiltin("fetch_git", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				url string
			)

			if err := starlark.UnpackArgs("fetch_git", args, kwargs,
				"url", &url,
			); err != nil {
				return starlark.None, err
			}

			return db.fetchGit(url)
		}),
		"register_script_fetcher": starlark.NewBuiltin("register_script_fetcher", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name  string
				f     *starlark.Function
				fArgs starlark.Tuple
			)

			if err := starlark.UnpackArgs("register_script_fetcher", args, kwargs,
				"name", &name,
				"f", &f,
				"fArgs", &fArgs,
			); err != nil {
				return starlark.None, err
			}

			if err := db.addScriptFetcher(name, f, fArgs); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}),
		"register_search_provider": starlark.NewBuiltin("register_search_provider", func(
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

			if err := starlark.UnpackArgs("register_search_provider", args, kwargs,
				"distro", &distro,
				"f", &f,
				"fArgs", &fArgs,
			); err != nil {
				return starlark.None, err
			}

			if err := db.addSearchProvider(distro, f, fArgs); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
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
		"parse_shell": starlark.NewBuiltin("parse_shell", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("parse_shell", args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			return parseShell(contents)
		}),
		"parse_yaml": starlark.NewBuiltin("parse_yaml", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("parse_yaml", args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			var body interface{}
			if err := yaml.Unmarshal([]byte(contents), &body); err != nil {
				return starlark.None, err
			}

			body = dyno.ConvertMapI2MapS(body)

			if b, err := json.Marshal(body); err != nil {
				return starlark.None, err
			} else {
				return starlark.Call(
					thread,
					starlarkjson.Module.Members["decode"],
					starlark.Tuple{starlark.String(b)},
					[]starlark.Tuple{},
				)
			}
		}),
		"parse_xml": starlark.NewBuiltin("parse_xml", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("parse_xml", args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			json, err := xj.Convert(strings.NewReader(contents))
			if err != nil {
				return starlark.None, err
			}

			return starlark.Call(
				thread,
				starlarkjson.Module.Members["decode"],
				starlark.Tuple{starlark.String(json.String())},
				[]starlark.Tuple{},
			)
		}),
		"parse_nix_derivation": starlark.NewBuiltin("parse_nix_derivation", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("parse_nix_derivation", args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			return parseNixDerivation(thread, contents)
		}),
		"parse_plist": starlark.NewBuiltin("parse_plist", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("parse_plist", args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			var obj any

			if _, err := plist.Unmarshal([]byte(contents), &obj); err != nil {
				return starlark.None, err
			}

			bytes, err := json.Marshal(obj)
			if err != nil {
				return starlark.None, err
			}

			return starlark.Call(
				thread,
				starlarkjson.Module.Members["decode"],
				starlark.Tuple{starlark.String(bytes)},
				[]starlark.Tuple{},
			)
		}),
		"open": starlark.NewBuiltin("open", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				filename string
			)

			if err := starlark.UnpackArgs("open", args, kwargs,
				"filename", &filename,
			); err != nil {
				return starlark.None, err
			}

			if !db.AllowLocal {
				return starlark.None, fmt.Errorf("open is only allowed if -allowLocal is passed")
			}

			f, err := os.Open(filename)
			if err != nil {
				return starlark.None, err
			}

			return &StarFile{f: f}, nil
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
		"json":     starlarkjson.Module,
		"__name__": starlark.String(name),
	}

	return globals, nil
}

func (db *PackageDatabase) LoadScript(filename string) error {
	thread := &starlark.Thread{
		Load: func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
			globals, err := db.getGlobals(module)
			if err != nil {
				return nil, err
			}

			ret, err := starlark.ExecFileOptions(&syntax.FileOptions{
				TopLevelControl: true,
				Recursion:       true,
				Set:             true,
			}, thread, module, nil, globals)
			if err != nil {
				return nil, err
			}

			return ret, nil
		},
	}

	globals, err := db.getGlobals("__main__")
	if err != nil {
		return err
	}

	_, err = starlark.ExecFileOptions(&syntax.FileOptions{
		TopLevelControl: true,
		Recursion:       true,
		Set:             true,
	}, thread, filename, nil, globals)
	if err != nil {
		return err
	}

	return nil
}

func (db *PackageDatabase) fetchWithKey(key string, fetcher *RepositoryFetcher) error {
	expireTime := 24 * time.Hour
	if db.ForceRefresh {
		expireTime = 0
	}

	err := db.Eif.CacheObjects(
		key, int(PackageMetadataVersionCurrent), expireTime,
		func(write func(obj any) error) error {
			slog.Info("fetching", "key", key)

			thread := &starlark.Thread{}

			_, err := starlark.Call(thread, fetcher.Func,
				append(starlark.Tuple{fetcher}, fetcher.Args...),
				[]starlark.Tuple{},
			)
			if err != nil {
				return err
			}

			for _, pkg := range fetcher.Packages {
				if err := write(pkg); err != nil {
					return err
				}
			}

			return nil
		},
		func(read func(obj any) error) error {
			for {
				pkg := NewPackage()

				err := read(pkg)
				if err == io.EOF {
					return nil
				} else if err != nil {
					return err
				} else {
					db.Packages = append(db.Packages, pkg)
				}
			}
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func (db *PackageDatabase) FetchAll() error {
	for _, fetcher := range db.Fetchers {
		key, err := fetcher.Key()
		if err != nil {
			return fmt.Errorf("failed to get fetcher key: %s", err)
		}

		if err := db.fetchWithKey(key, fetcher); err != nil {
			return fmt.Errorf("failed to load %s: %s", key, err)
		}
	}

	// Get the package index.
	db.PackageMap = make(map[string]*Package)
	for _, pkg := range db.Packages {
		db.PackageMap[pkg.Name.String()] = pkg
	}

	return nil
}

func (db *PackageDatabase) searchWithProviders(query PackageName, maxResults int) ([]*Package, error) {
	for _, searchProvider := range db.SearchProviders {
		if query.Distribution != searchProvider.Distribution {
			continue
		}

		results, err := searchProvider.Search(query, maxResults)
		if err != nil {
			return nil, err
		}

		return results, nil
	}

	return []*Package{}, nil
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

	if len(ret) == 0 {
		return db.searchWithProviders(query, maxResults)
	} else {
		return ret, nil
	}
}

func (db *PackageDatabase) Get(key string) (*Package, bool) {
	pkg, ok := db.PackageMap[key]

	return pkg, ok
}

func (db *PackageDatabase) GetBuildScript(script BuildScript) (starlark.Value, error) {
	for _, fetcher := range db.ScriptFetchers {
		if fetcher.Name == script.Name {
			thread := &starlark.Thread{}

			args := starlark.Tuple{fetcher}
			args = append(args, fetcher.Args...)
			for _, arg := range script.Args {
				args = append(args, starlark.String(arg))
			}

			ret, err := starlark.Call(thread, fetcher.Func,
				args,
				[]starlark.Tuple{},
			)
			if err != nil {
				return nil, err
			}

			return ret, nil
		}
	}

	return nil, fmt.Errorf("no build script fetcher defined for: %s", script.Name)
}

type InstallationPlan struct {
	db        *PackageDatabase
	installed map[string]bool
	Packages  []*Package
}

func (plan *InstallationPlan) checkName(name PackageName) bool {
	_, ok := plan.installed[name.ShortName()]

	return ok
}

func (plan *InstallationPlan) addName(name PackageName) error {
	// if plan.checkName(name) {
	// 	return fmt.Errorf("%s is already installed", name.ShortName())
	// }

	plan.installed[name.ShortName()] = true

	return nil
}

type ErrPackageNotFound PackageName

// Error implements error.
func (e ErrPackageNotFound) Error() string {
	return fmt.Sprintf("package %s not found", PackageName(e).String())
}

var (
	_ error = ErrPackageNotFound{}
)

func (plan *InstallationPlan) addPackage(query PackageName) error {
	if plan.checkName(query) {
		// Already installed.
		return nil
	}

	// Only look for 1 package.
	results, err := plan.db.Search(query, 1)
	if err != nil {
		return err
	}
	if len(results) != 1 {
		return ErrPackageNotFound(query)
	}

	pkg := results[0]

	// Add the names to the installed list.
	if err := plan.addName(pkg.Name); err != nil {
		return err
	}

	for _, alias := range pkg.Aliases {
		if err := plan.addName(alias); err != nil {
			return err
		}
	}

	// Add all dependencies.
outer:
	for _, depend := range pkg.Depends {
		for _, option := range depend {
			err := plan.addPackage(option)
			if _, ok := err.(ErrPackageNotFound); ok {
				continue
			} else if err != nil {
				return err
			}

			continue outer
		}

		return fmt.Errorf("could not find installation candidate among options: %+v", depend)
	}

	// Finally add the package.
	plan.Packages = append(plan.Packages, pkg)

	return nil
}

func (db *PackageDatabase) MakeInstallationPlan(packages []PackageName) (*InstallationPlan, error) {
	plan := &InstallationPlan{db: db, installed: make(map[string]bool)}

	for _, pkg := range packages {
		if err := plan.addPackage(pkg); err != nil {
			return nil, err
		}
	}

	return plan, nil
}
