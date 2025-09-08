package cli

import (
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/tinyrange/tinyrange/build/internal"

	buildhttp "github.com/tinyrange/tinyrange/build/http"
)

func Main() error {
	fs := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)

	addr := fs.String("addr", "127.0.0.1:8080", "Address to listen on")
	static := fs.String("static", "", "Path to static web files")

	fs.Parse(os.Args[1:])

	db, err := internal.New()
	if err != nil {
		return err
	}

	handler := buildhttp.New(db)

	mux := http.NewServeMux()

	mux.Handle("/api/", http.StripPrefix("/api/", handler))

	if *static != "" {
		slog.Info("serving static files", "path", *static)
		fs := http.FileServer(http.Dir(*static))
		mux.Handle("/", fs)
	} else {
		slog.Info("no static files configured")
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
