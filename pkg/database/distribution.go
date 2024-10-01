package database

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"

	"github.com/tinyrange/tinyrange/pkg/common"
)

func logHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("got request", "url", r.URL, "remote", r.RemoteAddr)
		h.ServeHTTP(w, r)
	})
}

func handler(f func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			slog.Error("got error from handler", "url", r.URL, "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

var validHash = regexp.MustCompile("[0-9a-f]{64}")

type distributionServer struct {
	db  *PackageDatabase
	mux *http.ServeMux
}

func (svr *distributionServer) validateHash(hash string) (string, error) {
	result := validHash.FindString(hash)
	if result == "" {
		return "", fmt.Errorf("bad hash")
	}

	return result, nil
}

func (svr *distributionServer) handleHealthCheck(w http.ResponseWriter, r *http.Request) error {
	if _, err := fmt.Fprintf(w, "OK"); err != nil {
		return err
	}

	return nil
}

func (svr *distributionServer) handleGetResult(w http.ResponseWriter, r *http.Request) error {
	hash := r.PathValue("hash")

	// First validate the hash. This will return only the hash and exclude any data after it.
	validated, err := svr.validateHash(hash)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return nil
	}

	// Then check if the result is redistributable
	redistributableFilename, err := svr.db.FilenameFromHash(validated, ".redistributable")
	if err != nil {
		return err
	}

	if exists, _ := common.Exists(redistributableFilename); !exists {
		http.Error(w, "not found", http.StatusNotFound)
		return nil
	}

	// Only then open the result file and serve it like normal.
	filename, err := svr.db.FilenameFromHash(validated, ".bin")
	if err != nil {
		return err
	}

	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file")
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat result file")
	}

	http.ServeContent(w, r, filename, info.ModTime(), f)

	return nil
}

func (db *PackageDatabase) RunDistributionServer(addr string) error {
	server := &distributionServer{
		db:  db,
		mux: http.NewServeMux(),
	}

	server.mux.HandleFunc("/health", handler(server.handleHealthCheck))
	server.mux.HandleFunc("/result/{hash}", handler(server.handleGetResult))

	fmt.Fprintf(os.Stdout, "Distribution Server Listening on http://%s\n", addr)

	return http.ListenAndServe(addr, logHandler(server.mux))
}
