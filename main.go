package main

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
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
)

func main() {
	flag.Parse()

	names := flag.Args()

	eif := core.NewEif("local/cache")

	if *allowLocal {
		slog.Warn("scripts can read local files with the -allowLocal flag")
	}

	pkgDb := &db.PackageDatabase{Eif: eif, AllowLocal: *allowLocal}

	start := time.Now()

	for _, name := range names {
		if err := pkgDb.LoadScript(name); err != nil {
			log.Fatal(err)
		}
	}

	if err := pkgDb.FetchAll(); err != nil {
		log.Fatal(err)
	}

	slog.Info("finished loading all repositories", "took", time.Since(start))

	if *makePlan != "" {
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
				if len(pkg.DownloadUrls) == 0 {
					log.Fatal("no download URLs", pkg)
				}

				if _, err := fmt.Fprintf(out, "  pkg distro:%s\n", pkg.DownloadUrls[0]); err != nil {
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
	} else {
		ui.RegisterHandlers(pkgDb, http.DefaultServeMux)

		slog.Info("http server listening", "addr", "http://"+*httpAddress)
		log.Fatal(http.ListenAndServe(*httpAddress, nil))
	}
}
