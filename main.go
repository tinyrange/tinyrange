package main

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tinyrange/pkg2/core"
	"github.com/tinyrange/pkg2/db"
	"github.com/tinyrange/pkg2/ui"
)

var (
	httpAddress       = flag.String("http", "localhost:5123", "the address to run a http web server on")
	buildScript       = flag.String("buildScript", "", "get the build script for a given package")
	makePlan          = flag.String("plan", "", "make a installation plan for a series of packages separated by commas")
	makeTinyrangePlan = flag.String("trplan", "", "make a tinyrange package definition executing the plan")
	allowLocal        = flag.Bool("allowLocal", false, "allow reading local files")
	forceRefresh      = flag.Bool("forceRefresh", false, "always refresh repository cache files")
	noParallel        = flag.Bool("noParallel", false, "use single threaded repository fetchers")
	cacheDir          = flag.String("cacheDir", "local/cache", "specify the cache dir to use")
	packageBase       = flag.String("packageBase", "", "the base directory to resolve packages from")
	test              = flag.Bool("test", false, "just fetch all repos")
	printPaths        = flag.Bool("paths", false, "print all package paths")
	database          = flag.String("database", "", "create a BoltDB database")
	testAll           = flag.Bool("testAll", false, "calculate a installation plan for every package in the index and print any packages that fails for")
	runRepl           = flag.Bool("repl", false, "start a REPL")
	enableDownloads   = flag.Bool("enableDownloads", false, "enable support for downloading packages. not recommended for public instances")
)

func main() {
	flag.Parse()

	names := flag.Args()

	eif := core.NewEif(*cacheDir)

	if *allowLocal {
		slog.Warn("scripts can read local files with the -allowLocal flag")
	}

	pkgDb := &db.PackageDatabase{
		Eif:             eif,
		AllowLocal:      *allowLocal,
		ForceRefresh:    *forceRefresh,
		NoParallel:      *noParallel,
		PackageBase:     *packageBase,
		EnableDownloads: *enableDownloads,

		ContentFetchers: make(map[string]*db.ContentFetcher),
	}

	if *database != "" {
		closer, err := pkgDb.OpenDatabase(*database)
		if err != nil {
			log.Fatal("failed to open database: ", err)
		}
		defer closer.Close()
	}

	for _, name := range names {
		if err := pkgDb.LoadScript(name); err != nil {
			log.Fatal("failed to load script: ", err)
		}
	}

	slog.Info("loaded scripts")

	if *runRepl {
		start := time.Now()

		if err := pkgDb.FetchAll(); err != nil {
			log.Fatal("failed to fetch: ", err)
		}

		slog.Info("finished loading all repositories", "took", time.Since(start), "packages", pkgDb.Count())

		if err := pkgDb.RunRepl(); err != nil {
			log.Fatal("failed to run REPL: ", err)
		}
	} else if *makePlan != "" {
		start := time.Now()

		if err := pkgDb.FetchAll(); err != nil {
			log.Fatal("failed to fetch: ", err)
		}

		slog.Info("finished loading all repositories", "took", time.Since(start), "packages", pkgDb.Count())

		var names []db.PackageName

		for _, token := range strings.Split(*makePlan, ",") {
			name, err := db.ParsePackageName(token)
			if err != nil {
				log.Fatal(err)
			}

			names = append(names, name)
		}

		plan, err := pkgDb.MakeInstallationPlan(names)
		if err != nil {
			log.Fatal(err)
		}

		if *makeTinyrangePlan != "" {
			out, err := os.Create(*makeTinyrangePlan)
			if err != nil {
				log.Fatal(err)
			}
			defer out.Close()

			if _, err := fmt.Fprintf(out, "pkgdef plan {\n"); err != nil {
				log.Fatal(err)
			}

			for _, pkg := range plan.Packages {
				if len(pkg.Downloaders) == 0 {
					log.Fatal("no download URLs", pkg)
				}

				if _, err := fmt.Fprintf(out, "  pkg distro:%s\n", pkg.Downloaders[0].Url); err != nil {
					log.Fatal(err)
				}
			}

			if _, err := fmt.Fprintf(out, "}\n"); err != nil {
				log.Fatal(err)
			}
		} else {
			for _, pkg := range plan.Packages {
				slog.Info("", "pkg", pkg.Id())
			}
		}
	} else if *buildScript != "" {
		start := time.Now()

		if err := pkgDb.FetchAll(); err != nil {
			log.Fatal("failed to fetch: ", err)
		}

		slog.Info("finished loading all repositories", "took", time.Since(start), "packages", pkgDb.Count())

		pkg, ok := pkgDb.Get(*buildScript)
		if !ok {
			log.Fatalf("could not find package: %s", *buildScript)
		}

		for _, script := range pkg.BuildScripts {
			scriptInfo, err := pkgDb.GetBuildScript(script)
			if err != nil {
				log.Fatal(err)
			}

			slog.Info("", "scriptInfo", scriptInfo)
		}
	} else if *test {
		start := time.Now()

		if err := pkgDb.FetchAll(); err != nil {
			log.Fatal("failed to fetch: ", err)
		}

		slog.Info("finished loading all repositories", "took", time.Since(start), "packages", pkgDb.Count())

		if *printPaths {
			for _, name := range pkgDb.AllNames() {
				fmt.Printf("%s\n", filepath.Join(append([]string{"local", "packages"}, name.Path()...)...))
			}
		} else if *testAll {
			if err := pkgDb.TestAllPackages(); err != nil {
				log.Fatal("failed to test all packages: ", err)
			}
		}
	} else {
		pkgDb.StartAutoRefresh(2, 2*time.Hour, *forceRefresh)

		ui.RegisterHandlers(pkgDb, http.DefaultServeMux)

		slog.Info("http server listening", "addr", "http://"+*httpAddress)
		log.Fatal(http.ListenAndServe(*httpAddress, nil))
	}
}
