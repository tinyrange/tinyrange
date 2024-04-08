package db

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	xj "github.com/basgys/goxml2json"
	"github.com/icza/dyno"
	"github.com/minio/sha256-simd"
	"github.com/schollz/progressbar/v3"
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

type RepositoryFetcherStatus int

func (status RepositoryFetcherStatus) String() string {
	switch status {
	case RepositoryFetcherStatusNotLoaded:
		return "Not Loaded"
	case RepositoryFetcherStatusLoading:
		return "Loading"
	case RepositoryFetcherStatusLoaded:
		return "Loaded"
	default:
		return "<unknown>"
	}
}

const (
	RepositoryFetcherStatusNotLoaded RepositoryFetcherStatus = iota
	RepositoryFetcherStatusLoading
	RepositoryFetcherStatusLoaded
	RepositoryFetcherStatusError
)

type RepositoryFetcher struct {
	db             *PackageDatabase
	Packages       []*Package
	Distributions  map[string]bool
	Distro         string
	Func           *starlark.Function
	Args           starlark.Tuple
	Status         RepositoryFetcherStatus
	updateMutex    sync.Mutex
	LastUpdateTime time.Duration
	LastUpdated    time.Time
}

func (r *RepositoryFetcher) Matches(query PackageName) bool {
	if query.Distribution != "" {
		_, ok := r.Distributions[query.Distribution]

		return ok
	}

	return true
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
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

func (fetcher *RepositoryFetcher) String() string {
	name := fetcher.Func.Name()

	var args []string
	for _, arg := range fetcher.Args {
		args = append(args, arg.String())
	}

	return fmt.Sprintf("%s(%s)", name, strings.Join(args, ", "))
}
func (*RepositoryFetcher) Type() string { return "RepositoryFetcher" }
func (*RepositoryFetcher) Hash() (uint32, error) {
	return 0, fmt.Errorf("RepositoryFetcher is not hashable")
}
func (*RepositoryFetcher) Truth() starlark.Bool { return starlark.True }
func (*RepositoryFetcher) Freeze()              {}

func (fetcher *RepositoryFetcher) fetchWithKey(eif *core.EnvironmentInterface, key string, forceRefresh bool) error {
	// Only allow a single thread to update a fetcher at the same time.
	fetcher.updateMutex.Lock()
	defer fetcher.updateMutex.Unlock()

	fetcher.LastUpdated = time.Now()

	fetcher.Status = RepositoryFetcherStatusLoading

	expireTime := 8 * time.Hour
	if forceRefresh {
		expireTime = 0
	}

	err := eif.CacheObjects(
		key, int(PackageMetadataVersionCurrent), expireTime,
		func(write func(obj any) error) error {
			slog.Info("fetching", "fetcher", fetcher.String())

			thread := &starlark.Thread{}

			_, err := starlark.Call(thread, fetcher.Func,
				append(starlark.Tuple{fetcher}, fetcher.Args...),
				[]starlark.Tuple{},
			)
			if err != nil {
				if sErr, ok := err.(*starlark.EvalError); ok {
					slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
				}
				return fmt.Errorf("error calling user callback: %s", err)
			}

			for _, pkg := range fetcher.Packages {
				if err := write(pkg); err != nil {
					return fmt.Errorf("failed to write package: %s", err)
				}
			}

			return nil
		},
		func(read func(obj any) error) error {
			fetcher.Packages = []*Package{}

			for {
				pkg := NewPackage()

				err := read(pkg)
				if err == io.EOF {
					return nil
				} else if err != nil {
					return err
				} else {
					fetcher.Packages = append(fetcher.Packages, pkg)

					// Add to the distribution index.
					fetcher.Distributions[pkg.Name.Distribution] = true
					for _, alias := range pkg.Aliases {
						fetcher.Distributions[alias.Distribution] = true
					}
				}
			}
		},
	)
	if err != nil {
		fetcher.Status = RepositoryFetcherStatusError

		return err
	}

	fetcher.Status = RepositoryFetcherStatusLoaded

	fetcher.LastUpdateTime = time.Since(fetcher.LastUpdated)

	return nil
}

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
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
	packageMap      map[string]*Package
	packageMapMutex sync.Mutex
	AllowLocal      bool
	ForceRefresh    bool
	NoParallel      bool
	PackageBase     string
}

func (db *PackageDatabase) addRepositoryFetcher(distro string, f *starlark.Function, args starlark.Tuple) error {
	db.Fetchers = append(db.Fetchers, &RepositoryFetcher{
		db:            db,
		Distro:        distro,
		Func:          f,
		Args:          args,
		Distributions: make(map[string]bool),
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
				url          string
				expectedSize int64
			)

			if err := starlark.UnpackArgs("fetch_http", args, kwargs,
				"url", &url,
				"expected_size?", &expectedSize,
			); err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
			}

			f, err := db.Eif.HttpGetReader(url, core.HttpOptions{
				ExpectedSize: expectedSize,
			})
			if err == core.ErrNotFound {
				return starlark.None, nil
			} else if err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
			}

			if err := db.addScriptFetcher(name, f, fArgs); err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
			}

			if err := db.addSearchProvider(distro, f, fArgs); err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
			}

			if err := db.addRepositoryFetcher(distro, f, fArgs); err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
			}

			var body interface{}
			if err := yaml.Unmarshal([]byte(contents), &body); err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
			}

			body = dyno.ConvertMapI2MapS(body)

			if b, err := json.Marshal(body); err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
			}

			json, err := xj.Convert(strings.NewReader(contents))
			if err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
			}

			var obj any

			if _, err := plist.Unmarshal([]byte(contents), &obj); err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
			}

			bytes, err := json.Marshal(obj)
			if err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
			}

			if !db.AllowLocal {
				return starlark.None, fmt.Errorf("open is only allowed if -allowLocal is passed")
			}

			f, err := os.Open(filename)
			if err != nil {
				return starlark.None, fmt.Errorf("TODO: %s", err)
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
				return starlark.None, fmt.Errorf("TODO: %s", err)
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

			filename := filepath.Join(db.PackageBase, module)

			ret, err := starlark.ExecFileOptions(&syntax.FileOptions{
				TopLevelControl: true,
				Recursion:       true,
				Set:             true,
			}, thread, filename, nil, globals)
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

	filename = filepath.Join(db.PackageBase, filename)

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

func (db *PackageDatabase) FetchAll() error {
	wg := sync.WaitGroup{}
	done := make(chan bool)
	errors := make(chan error)

	pb := progressbar.Default(int64(len(db.Fetchers)))
	defer pb.Close()

	for _, fetcher := range db.Fetchers {
		key, err := fetcher.Key()
		if err != nil {
			return fmt.Errorf("failed to get fetcher key: %s", err)
		}

		wg.Add(1)
		if db.NoParallel {
			if err := fetcher.fetchWithKey(db.Eif, key, db.ForceRefresh); err != nil {
				return fmt.Errorf("failed to load %s: %s", fetcher.String(), err)
			}
		} else {
			go func(key string, fetcher *RepositoryFetcher) {
				defer wg.Done()
				if err := fetcher.fetchWithKey(db.Eif, key, db.ForceRefresh); err != nil {
					errors <- fmt.Errorf("failed to load %s: %s", fetcher.String(), err)
				}

				pb.Add(1)
			}(key, fetcher)
		}
	}

	go func() {
		wg.Wait()

		done <- true
	}()

	select {
	case err := <-errors:
		return err
	case <-done:
		break
	}

	// Get the package index.
	db.packageMapMutex.Lock()
	defer db.packageMapMutex.Unlock()
	db.packageMap = make(map[string]*Package)

	for _, fetcher := range db.Fetchers {
		for _, pkg := range fetcher.Packages {
			db.packageMap[pkg.Name.String()] = pkg
		}
	}

	return nil
}

// Start a series of go routines to automatically refresh each package fetcher every refreshTime.
func (db *PackageDatabase) StartAutoRefresh(maxParallelFetchers int, refreshTime time.Duration, forceRefresh bool) {
	// Initialize the package map.
	db.packageMap = make(map[string]*Package)

	updateRequests := make(chan struct {
		fetcher *RepositoryFetcher
		force   bool
	}, maxParallelFetchers)

	for _, fetcher := range db.Fetchers {
		go func(fetcher *RepositoryFetcher) {
			ticker := time.NewTicker(refreshTime)

			updateRequests <- struct {
				fetcher *RepositoryFetcher
				force   bool
			}{
				fetcher: fetcher,
				force:   forceRefresh,
			}

			for range ticker.C {
				updateRequests <- struct {
					fetcher *RepositoryFetcher
					force   bool
				}{
					fetcher: fetcher,
					force:   true,
				}
			}
		}(fetcher)
	}

	for i := 0; i < maxParallelFetchers; i++ {
		go func() {
			for {
				updateRequest := <-updateRequests

				key, err := updateRequest.fetcher.Key()
				if err != nil {
					slog.Warn("could not get fetcher key", "fetcher", updateRequest.fetcher.String(), "error", err)
					continue
				}

				if err := updateRequest.fetcher.fetchWithKey(db.Eif, key, updateRequest.force); err != nil {
					slog.Warn("could not get update fetcher", "fetcher", updateRequest.fetcher.String(), "error", err)
					continue
				}

				{
					db.packageMapMutex.Lock()

					for _, pkg := range updateRequest.fetcher.Packages {
						db.packageMap[pkg.Name.String()] = pkg
					}

					db.packageMapMutex.Unlock()
				}
			}
		}()
	}
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

outer:
	for _, fetcher := range db.Fetchers {
		// If the fetcher doesn't possibly match this query then early out.
		if !fetcher.Matches(query) {
			continue
		}

		// Search though each package.
		for _, pkg := range fetcher.Packages {
			if pkg.Matches(query) {
				ret = append(ret, pkg)
				if maxResults != 0 && len(ret) >= maxResults {
					break outer
				}
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
	db.packageMapMutex.Lock()
	defer db.packageMapMutex.Unlock()

	pkg, ok := db.packageMap[key]

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

func (db *PackageDatabase) Count() int64 {
	var ret int64 = 0

	for _, fetcher := range db.Fetchers {
		ret += int64(len(fetcher.Packages))
	}

	return ret
}

type FetcherStatus struct {
	Name           string
	Status         RepositoryFetcherStatus
	PackageCount   int
	LastUpdated    time.Time
	LastUpdateTime time.Duration
}

func (db *PackageDatabase) FetcherStatus() ([]FetcherStatus, error) {
	var ret []FetcherStatus

	for _, fetcher := range db.Fetchers {
		ret = append(ret, FetcherStatus{
			Name:           fetcher.String(),
			Status:         fetcher.Status,
			PackageCount:   len(fetcher.Packages),
			LastUpdated:    fetcher.LastUpdated,
			LastUpdateTime: fetcher.LastUpdateTime,
		})
	}

	return ret, nil
}

func (db *PackageDatabase) WriteNames(w io.Writer) error {
	enc := json.NewEncoder(w)

	for _, fetcher := range db.Fetchers {
		for _, pkg := range fetcher.Packages {
			if err := enc.Encode(pkg.Name); err != nil {
				return err
			}

			for _, alias := range pkg.Aliases {
				if err := enc.Encode(alias); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
