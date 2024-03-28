package main

import (
	"flag"
	"log"
	"log/slog"
	"net/http"

	"github.com/tinyrange/pkg2/core"
	"github.com/tinyrange/pkg2/db"
	"github.com/tinyrange/pkg2/ui"
)

var (
	httpAddress = flag.String("http", "localhost:5123", "the address to run a http web server on")
	buildScript = flag.String("buildScript", "", "get the build script for a given package")
)

func main() {
	flag.Parse()

	names := flag.Args()

	eif := core.NewEif("local/cache")

	db := &db.PackageDatabase{Eif: eif}

	for _, name := range names {
		if err := db.LoadScript(name); err != nil {
			log.Fatal(err)
		}
	}

	if err := db.FetchAll(); err != nil {
		log.Fatal(err)
	}

	if *buildScript != "" {
		pkg, ok := db.Get(*buildScript)
		if !ok {
			log.Fatalf("could not find package: %s", *buildScript)
		}

		for _, script := range pkg.BuildScripts {
			scriptInfo, err := db.GetBuildScript(script)
			if err != nil {
				log.Fatal(err)
			}

			slog.Info("", "scriptInfo", scriptInfo)
		}
	} else {
		ui.RegisterHandlers(db, http.DefaultServeMux)

		slog.Info("http server listening", "addr", "http://"+*httpAddress)
		log.Fatal(http.ListenAndServe(*httpAddress, nil))
	}
}
