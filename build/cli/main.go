package cli

import (
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/tinyrange/tinyrange/build/cache/memory"
	"github.com/tinyrange/tinyrange/build/internal"

	buildhttp "github.com/tinyrange/tinyrange/build/http"
)

func Main() error {
	fs := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)

	addr := fs.String("addr", "127.0.0.1:8080", "Address to listen on")
	static := fs.String("static", "", "Path to static web files")
	apiOnly := fs.Bool("api-only", false, "Don't serve any files")
	insecureCors := fs.Bool("insecure-cors", false, "Add a middleware to set insecure CORS headers")

	fs.Parse(os.Args[1:])

	cache := memory.NewCache()

	db, err := internal.New(cache)
	if err != nil {
		return err
	}
	var mux http.Handler
	if *apiOnly {
		mux = buildhttp.New(db, "")
	} else {
		handler := buildhttp.New(db, "/api")

		serveMux := http.NewServeMux()

		if *static != "" {
			slog.Info("serving static files", "path", *static)
			fs := http.FileServer(http.Dir(*static))
			serveMux.Handle("/", fs)
		} else {
			slog.Info("no static files configured")
		}

		serveMux.Handle("/api/", handler)

		mux = serveMux
	}

	if *insecureCors {
		oldMux := mux
		mux = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			oldMux.ServeHTTP(w, r)
		})
	}

	listen, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	defer listen.Close()

	slog.Info("listening", "addr", listen.Addr().String())

	srv := &http.Server{
		Handler: mux,
	}

	return srv.Serve(listen)
}
