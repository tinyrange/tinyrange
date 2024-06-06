package db

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/pkg2/pkg/core"
	"github.com/tinyrange/pkg2/pkg/memtar"
	bolt "go.etcd.io/bbolt"
	"go.starlark.net/repl"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type ScriptFile struct {
	filename string
}

func (*ScriptFile) String() string        { return "ScriptFile" }
func (*ScriptFile) Type() string          { return "ScriptFile" }
func (*ScriptFile) Hash() (uint32, error) { return 0, fmt.Errorf("ScriptFile is not hashable") }
func (*ScriptFile) Truth() starlark.Bool  { return starlark.True }
func (*ScriptFile) Freeze()               {}

var (
	_ starlark.Value = &ScriptFile{}
)

type QueryOptions struct {
	ExcludeRecommends  bool
	MaxResults         int
	PreferArchitecture string
}

type PackageDatabase struct {
	Eif             *core.EnvironmentInterface
	Fetchers        []*RepositoryFetcher
	ScriptFetchers  []*ScriptFetcher
	SearchProviders []*SearchProvider
	ContentFetchers map[string]*ContentFetcher
	packageMap      map[string]*Package
	packageMapMutex sync.Mutex
	AllowLocal      bool
	ForceRefresh    bool
	NoParallel      bool
	PackageBase     string
	EnableDownloads bool
	ScriptMode      bool
	Rebuild         bool

	builtDefinitions map[string]bool

	scriptFunction starlark.Value
	db             *bolt.DB
}

func (db *PackageDatabase) addRepositoryFetcher(distro string, f *starlark.Function, args starlark.Tuple) error {
	db.Fetchers = append(db.Fetchers, &RepositoryFetcher{
		db:      db,
		Distro:  distro,
		Func:    f,
		Args:    args,
		Counter: core.NewCounter(),
	})

	return nil
}

func (db *PackageDatabase) addContentFetcher(name string, f *starlark.Function, args starlark.Tuple) error {
	db.ContentFetchers[name] = &ContentFetcher{
		Func: f,
		Args: args,
	}

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

func (db *PackageDatabase) RunScript() error {
	thread := &starlark.Thread{
		Print: func(thread *starlark.Thread, msg string) {
			fmt.Fprintf(os.Stdout, "%s\n", msg)
		},
	}

	_, err := starlark.Call(thread, db.scriptFunction, starlark.Tuple{db}, []starlark.Tuple{})
	if err != nil {
		if sErr, ok := err.(*starlark.EvalError); ok {
			slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
		}
		return err
	}

	return nil
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
				GlobalReassign:  true,
			}, thread, filename, nil, globals)
			if err != nil {
				if sErr, ok := err.(*starlark.EvalError); ok {
					slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
				}
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

	globals["__file__"] = &ScriptFile{filename: filename}

	_, err = starlark.ExecFileOptions(&syntax.FileOptions{
		TopLevelControl: true,
		Recursion:       true,
		Set:             true,
		GlobalReassign:  true,
	}, thread, filename, nil, globals)
	if err != nil {
		if sErr, ok := err.(*starlark.EvalError); ok {
			slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
		}
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
		key := fetcher.Key()

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

				key := updateRequest.fetcher.Key()

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

func (db *PackageDatabase) searchWithProviders(query PackageName, opts QueryOptions) ([]*Package, error) {
	for _, searchProvider := range db.SearchProviders {
		if query.Distribution != searchProvider.Distribution {
			continue
		}

		results, err := searchProvider.Search(query, opts.MaxResults)
		if err != nil {
			return nil, err
		}

		return results, nil
	}

	return []*Package{}, nil
}

func (db *PackageDatabase) Search(query PackageName, opts QueryOptions) ([]*Package, error) {
	var ret []*Package

	// slog.Info("search", "query", query)

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
				if opts.MaxResults != 0 && len(ret) >= opts.MaxResults {
					break outer
				}
			}
		}
	}

	if len(ret) == 0 {
		return db.searchWithProviders(query, opts)
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
	db                *PackageDatabase
	installed         map[string]string
	installedPackages map[string]*Package
	Packages          []*Package
	queryOptions      QueryOptions
	dependencyGraph   [][2]*Package
}

func (plan *InstallationPlan) checkName(name PackageName) (string, bool) {
	ver, ok := plan.installed[name.ShortName()]

	return ver, ok
}

func (plan *InstallationPlan) getInstalled(name PackageName) (*Package, bool) {
	pkg, ok := plan.installedPackages[name.ShortName()]

	return pkg, ok
}

func (plan *InstallationPlan) addName(pkg *Package, name PackageName) error {
	// if plan.checkName(name) {
	// 	return fmt.Errorf("%s is already installed", name.ShortName())
	// }

	plan.installedPackages[name.ShortName()] = pkg
	plan.installed[name.ShortName()] = name.Version

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

func (plan *InstallationPlan) pickPackage(query PackageName, results []*Package, filtered bool) (*Package, error) {
	if len(results) == 1 {
		return results[0], nil
	}

	// Check if we have a preferred architecture.
	if plan.queryOptions.PreferArchitecture != "" && !filtered {
		archQuery := query
		archQuery.Architecture = plan.queryOptions.PreferArchitecture

		var filtered []*Package

		for _, pkg := range results {
			if pkg.Matches(archQuery) {
				filtered = append(filtered, pkg)
			}
		}

		// slog.Info("preferred", "filtered", filtered)

		if len(filtered) > 0 {
			return plan.pickPackage(query, filtered, true)
		}
	}

	// slog.Info("got multiple installation candidates", "query", query, "results", results)

	return results[0], nil
}

func (plan *InstallationPlan) addPackage(parent *Package, query PackageName) ([]*Package, error) {
	if pkg, ok := plan.getInstalled(query); ok {
		// Already installed.

		plan.dependencyGraph = append(plan.dependencyGraph, [2]*Package{parent, pkg})

		return nil, nil
	}

	var added []*Package

	// Only look for 1 package.
	opts := plan.queryOptions
	results, err := plan.db.Search(query, opts)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, ErrPackageNotFound(query)
	}

	// Pick a package from the list of candidates.
	pkg, err := plan.pickPackage(query, results, false)
	if err != nil {
		return nil, err
	}

	plan.dependencyGraph = append(plan.dependencyGraph, [2]*Package{parent, pkg})

	added = append(added, pkg)

	// Add the names to the installed list.
	if err := plan.addName(pkg, pkg.Name); err != nil {
		return nil, err
	}

	// Check for conflicts.
	for _, conflict := range pkg.Conflicts {
		for _, option := range conflict {
			ver, ok := plan.checkName(option)
			if ok && versionMatches(ver, option.Version) {
				slog.Error("conflict", "pkg", pkg, "conflicts", pkg.Conflicts)
				return nil, fmt.Errorf("found conflict between %s and %s", query, option)
			}
		}
	}

	// Add aliases afterwards.
	// This makes sure the package is not conflicting with itself.
	for _, alias := range pkg.Aliases {
		if err := plan.addName(pkg, alias); err != nil {
			return nil, err
		}
	}

	// Add all dependencies.
outer:
	for _, depend := range pkg.Depends {
		for _, option := range depend {
			if option.Recommended && plan.queryOptions.ExcludeRecommends {
				continue
			}

			newAdded, err := plan.addPackage(pkg, option)
			if _, ok := err.(ErrPackageNotFound); ok {
				continue
			} else if err != nil {
				return nil, fmt.Errorf("failed to add package for %s: %s", pkg.String(), err)
			}

			added = append(added, newAdded...)

			continue outer
		}

		return nil, fmt.Errorf("could not find installation candidate among options: %+v", depend)
	}

	// Finally add the package.
	plan.Packages = append(plan.Packages, pkg)

	return added, nil
}

func (plan *InstallationPlan) dumpGraph() error {
	fmt.Printf("digraph G {\n")
	for _, edge := range plan.dependencyGraph {
		if edge[0] != nil {
			fmt.Printf("%s -> %s\n", edge[0].Name.String(), edge[1].Name.String())
		} else {
			fmt.Printf("<nil> -> %s\n", edge[1].Name.String())
		}
	}
	fmt.Printf("}\n")

	return nil
}

// Attr implements starlark.HasAttrs.
func (plan *InstallationPlan) Attr(name string) (starlark.Value, error) {
	if name == "add" {
		return starlark.NewBuiltin("Database.plan", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var names []PackageName
			for _, arg := range args {
				if name, ok := arg.(PackageName); ok {
					names = append(names, name)
				} else if pkg, ok := arg.(*Package); ok {
					names = append(names, pkg.Name)
				} else {
					return starlark.None, fmt.Errorf("expected Name|Package got %s", arg.Type())
				}
			}

			var added []starlark.Value

			for _, name := range names {
				addedNew, err := plan.addPackage(nil, name)
				if err != nil {
					return starlark.None, err
				}

				for _, pkg := range addedNew {
					added = append(added, pkg)
				}
			}

			return starlark.NewList(added), nil
		}), nil
	} else if name == "dump_graph" {
		return starlark.NewBuiltin("Database.dump_graph", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			if err := plan.dumpGraph(); err != nil {
				return starlark.None, err
			}
			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (plan *InstallationPlan) AttrNames() []string {
	return []string{"add", "dump_graph"}
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

func (db *PackageDatabase) MakeInstallationPlan(packages []PackageName, opts QueryOptions) (*InstallationPlan, error) {
	plan := db.MakeIncrementalPlanner(opts)

	for _, pkg := range packages {
		if _, err := plan.addPackage(nil, pkg); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

func (db *PackageDatabase) MakeIncrementalPlanner(opts QueryOptions) *InstallationPlan {
	return &InstallationPlan{
		db:                db,
		installed:         make(map[string]string),
		installedPackages: make(map[string]*Package),
		queryOptions:      opts,
	}
}

func (db *PackageDatabase) Count() int64 {
	var ret int64 = 0

	for _, fetcher := range db.Fetchers {
		ret += int64(len(fetcher.Packages))
	}

	return ret
}

type FetcherStatus struct {
	Key            string
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
			Key:            fetcher.Key(),
			Name:           fetcher.String(),
			Status:         fetcher.Status,
			PackageCount:   len(fetcher.Packages),
			LastUpdated:    fetcher.LastUpdated,
			LastUpdateTime: fetcher.LastUpdateTime,
		})
	}

	return ret, nil
}

func (db *PackageDatabase) GetFetcher(key string) (*RepositoryFetcher, error) {
	for _, fetcher := range db.Fetchers {
		if fetcher.Key() == key {
			return fetcher, nil
		}
	}

	return nil, fmt.Errorf("fetcher not found")
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

func (db *PackageDatabase) AllNames() []PackageName {
	var ret []PackageName

	for _, fetcher := range db.Fetchers {
		for _, pkg := range fetcher.Packages {
			ret = append(ret, pkg.Name)
		}
	}

	return ret
}

func (db *PackageDatabase) DistributionList() []string {
	set := map[string]bool{"": true}

	for _, fetcher := range db.Fetchers {
		if fetcher.Distributions == nil {
			continue
		}

		for distro := range fetcher.Distributions {
			set[distro] = true
		}
	}

	var ret []string
	for name := range set {
		ret = append(ret, name)
	}
	slices.Sort(ret)
	return ret
}

func (db *PackageDatabase) ArchitectureList() []string {
	set := map[string]bool{"": true}

	for _, fetcher := range db.Fetchers {
		if fetcher.Architectures == nil {
			continue
		}

		for distro := range fetcher.Architectures {
			set[distro] = true
		}
	}

	var ret []string
	for name := range set {
		ret = append(ret, name)
	}
	slices.Sort(ret)
	return ret
}

func (db *PackageDatabase) OpenDatabase(filename string) (io.Closer, error) {
	var err error

	db.db, err = bolt.Open(filename, os.FileMode(0755), bolt.DefaultOptions)
	if err != nil {
		return nil, err
	}

	return db.db, nil
}

func (db *PackageDatabase) TestAllPackages() error {
	pb := progressbar.Default(db.Count())

	var broken int64 = 0
	var working int64 = 0

	for _, fetcher := range db.Fetchers {
		for _, pkg := range fetcher.Packages {
			planSearch := []PackageName{pkg.Name}

			_, err := db.MakeInstallationPlan(planSearch, QueryOptions{})
			if err != nil {
				broken += 1
				slog.Warn("failed to make installation plan", "package", pkg.Name, "error", err)
			} else {
				working += 1
				// slog.Info("made installation plan for", "package", pkg.Name)
			}

			pb.Add(1)
		}
	}

	slog.Info("finished testing installation plans for all packages", "working", working, "broken", broken, "total", db.Count())

	return nil
}

func (db *PackageDatabase) RunRepl() error {
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
				GlobalReassign:  true,
			}, thread, filename, nil, globals)
			if err != nil {
				if sErr, ok := err.(*starlark.EvalError); ok {
					slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
				}
				return nil, err
			}

			return ret, nil
		},
	}

	globals, err := db.getGlobals("__repl__")
	if err != nil {
		return err
	}

	repl.REPLOptions(&syntax.FileOptions{Set: true, While: true, TopLevelControl: true, GlobalReassign: true}, thread, globals)

	return nil
}

func (db *PackageDatabase) getPackageDownloadDefinition(pkg *Package, downloader Downloader) (*BuildDef, error) {
	fetcher, ok := db.ContentFetchers[downloader.Name]
	if !ok {
		return nil, fmt.Errorf("could not find fetcher: %s", downloader.Name)
	}

	return fetcher.GetDefinition(db, pkg, downloader.Url), nil
}

func (db *PackageDatabase) GetPackageContents(pkg *Package, downloader Downloader) (memtar.TarReader, error) {
	def, err := db.getPackageDownloadDefinition(pkg, downloader)
	if err != nil {
		return nil, err
	}

	filename, err := db.Build(def)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	return ReadArchive(f, ".tar", 0)
}

func (db *PackageDatabase) FetchParallel(packages []*Package) (memtar.TarReader, error) {
	var wg sync.WaitGroup

	var ret memtar.ArrayReader

	done := make(chan bool)
	archives := make(chan memtar.TarReader)
	errors := make(chan error)

	for _, pkg := range packages {
		wg.Add(1)

		go func(pkg *Package) {
			defer wg.Done()

			if len(pkg.Downloaders) == 0 {
				errors <- fmt.Errorf("package %s has no downloader", pkg)
				return
			}

			dl := pkg.Downloaders[0]

			contents, err := db.GetPackageContents(pkg, dl)
			if err != nil {
				errors <- err
				return
			}

			archives <- contents
		}(pkg)
	}

	go func() {
		wg.Wait()

		done <- true
	}()

	for {
		select {
		case err := <-errors:
			return nil, err
		case archive := <-archives:
			ret = append(ret, archive.Entries()...)
		case <-done:
			return ret, nil
		}
	}
}

// Attr implements starlark.HasAttrs.
func (db *PackageDatabase) Attr(name string) (starlark.Value, error) {
	if name == "query" {
		return starlark.NewBuiltin("Database.query", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name       PackageName
				maxResults int
			)

			includeRecommends := true

			if err := starlark.UnpackArgs("Database.query", args, kwargs,
				"name", &name,
				"recommended?", &includeRecommends,
				"max_results?", &maxResults,
			); err != nil {
				return starlark.None, err
			}

			results, err := db.Search(name, QueryOptions{
				MaxResults:        maxResults,
				ExcludeRecommends: !includeRecommends,
			})
			if err != nil {
				return starlark.None, err
			}

			var ret []starlark.Value

			for _, result := range results {
				ret = append(ret, result)
			}

			return starlark.NewList(ret), nil
		}), nil
	} else if name == "plan" {
		return starlark.NewBuiltin("Database.plan", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var names []PackageName
			for _, arg := range args {
				if name, ok := arg.(PackageName); ok {
					names = append(names, name)
				} else if pkg, ok := arg.(*Package); ok {
					names = append(names, pkg.Name)
				} else {
					return starlark.None, fmt.Errorf("expected Name|Package got %s", arg.Type())
				}
			}

			var (
				excludeRecommends  bool
				preferArchitecture string
			)

			if err := starlark.UnpackArgs(fn.Name(), starlark.Tuple{}, kwargs,
				"recommends?", &excludeRecommends,
				"prefer_architecture?", &preferArchitecture,
			); err != nil {
				return starlark.None, err
			}

			plan, err := db.MakeInstallationPlan(names, QueryOptions{
				ExcludeRecommends:  excludeRecommends,
				PreferArchitecture: preferArchitecture,
			})
			if err != nil {
				return starlark.None, err
			}

			var ret []starlark.Value
			for _, pkg := range plan.Packages {
				ret = append(ret, pkg)
			}

			return starlark.NewList(ret), nil
		}), nil
	} else if name == "incremental_plan" {
		return starlark.NewBuiltin("Database.incremental_plan", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				excludeRecommends  bool
				preferArchitecture string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"recommends?", &excludeRecommends,
				"prefer_architecture?", &preferArchitecture,
			); err != nil {
				return starlark.None, err
			}

			return db.MakeIncrementalPlanner(QueryOptions{
				ExcludeRecommends:  excludeRecommends,
				PreferArchitecture: preferArchitecture,
			}), nil
		}), nil
	} else if name == "download" {
		return starlark.NewBuiltin("Database.download", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var pkg *Package

			if err := starlark.UnpackArgs("Database.download", args, kwargs,
				"pkg", &pkg,
			); err != nil {
				return starlark.None, err
			}

			if len(pkg.Downloaders) == 0 {
				return starlark.None, fmt.Errorf("package has no downloaders")
			}

			dl := pkg.Downloaders[0]

			ents, err := db.GetPackageContents(pkg, dl)
			if err != nil {
				return starlark.None, err
			}

			return &StarArchive{r: ents, name: "<download_archive>"}, nil
		}), nil
	} else if name == "download_all" {
		return starlark.NewBuiltin("Database.download_all", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var packages []*Package
			for _, arg := range args {
				if pkg, ok := arg.(*Package); ok {
					packages = append(packages, pkg)
				} else {
					return starlark.None, fmt.Errorf("expected Package got %s", pkg.Type())
				}
			}

			ents, err := db.FetchParallel(packages)
			if err != nil {
				return starlark.None, err
			}

			return &StarArchive{r: ents, name: "<download_archive>"}, nil
		}), nil
	} else if name == "download_def" {
		return starlark.NewBuiltin("Database.download_def", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var pkg *Package

			if err := starlark.UnpackArgs("Database.download_def", args, kwargs,
				"pkg", &pkg,
			); err != nil {
				return starlark.None, err
			}

			return db.getPackageDownloadDefinition(pkg, pkg.Downloaders[0])
		}), nil
	} else if name == "build" {
		return starlark.NewBuiltin("Database.build", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				tag         starlark.Tuple
				builder     *starlark.Function
				builderArgs starlark.Tuple
			)

			if err := starlark.UnpackArgs("Database.build", args, kwargs,
				"tag", &tag,
				"builder?", &builder,
				"builderArgs?", &builderArgs,
			); err != nil {
				return starlark.None, err
			}

			return db.build(tag, builder, builderArgs)
		}), nil
	} else if name == "args" {
		var ret starlark.Tuple

		for _, val := range flag.Args() {
			ret = append(ret, starlark.String(val))
		}

		return ret, nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (db *PackageDatabase) AttrNames() []string {
	return []string{"query"}
}

func (*PackageDatabase) String() string        { return "Database" }
func (*PackageDatabase) Type() string          { return "Database" }
func (*PackageDatabase) Hash() (uint32, error) { return 0, fmt.Errorf("Database is not hashable") }
func (*PackageDatabase) Truth() starlark.Bool  { return starlark.True }
func (*PackageDatabase) Freeze()               {}

var (
	_ starlark.Value    = &PackageDatabase{}
	_ starlark.HasAttrs = &PackageDatabase{}
)
