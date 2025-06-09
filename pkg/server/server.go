package server

import (
	"encoding/json"
	"net/http"
	"runtime/debug"

	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/log"
)

const API_VERSION = "0.1.0"

type indexResponse struct {
	ApiVersion       string `json:"api_version"`
	TinyRangeVersion string `json:"tinyrange_version"`
}

type Server struct {
	db   common.PackageDatabase
	mux  *http.ServeMux
	http http.Server
}

func (s *Server) ListenAndServe(address string) error {
	s.http.Addr = address
	s.http.Handler = s.mux

	log.Info("Starting TinyRange server", "address", "http://"+address)

	return s.http.ListenAndServe()
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		http.Error(w, "failed to read build info", http.StatusInternalServerError)
		return
	}

	resp := indexResponse{
		ApiVersion:       API_VERSION,
		TinyRangeVersion: buildInfo.Main.Version,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	resp := map[string]string{
		"status": "ok",
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleGetAllDefinitions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	hashes, err := s.db.Builder().Filesystem().GetAllHashes()
	if err != nil {
		http.Error(w, "failed to get all hashes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var response []string
	for _, hash := range hashes {
		response = append(response, hash.String())
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func New(db common.PackageDatabase) *Server {
	s := &Server{
		db:  db,
		mux: http.NewServeMux(),
	}

	// General Server Information
	s.mux.HandleFunc("GET /info", s.handleIndex)
	s.mux.HandleFunc("GET /healthz", s.handleHealthCheck)

	// Build API Endpoints
	s.mux.HandleFunc("GET /build", s.handleGetAllDefinitions)

	// Register the standard build2 handler for reading the build directory
	build2.RegisterBuildDirectoryHandler(s.mux, db.Builder().Filesystem().(build2.BuildCacheFilesystem), "/build", true)

	return s
}
