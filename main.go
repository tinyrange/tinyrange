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

	ui.RegisterHandlers(db, http.DefaultServeMux)

	slog.Info("http server listening", "addr", "http://"+*httpAddress)
	log.Fatal(http.ListenAndServe(*httpAddress, nil))
}
