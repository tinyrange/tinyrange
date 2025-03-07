package remote

import "net/http"

type BuilderHTTPMux struct {
	mux *http.ServeMux
}

func (b *BuilderHTTPMux) getDefinition(w http.ResponseWriter, r *http.Request, hash string) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (b *BuilderHTTPMux) getReceipt(w http.ResponseWriter, r *http.Request, hash string) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (b *BuilderHTTPMux) build(w http.ResponseWriter, r *http.Request, hash string) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (b *BuilderHTTPMux) getOutput(w http.ResponseWriter, r *http.Request, hash, name string) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (b *BuilderHTTPMux) list(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (b *BuilderHTTPMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mux.ServeHTTP(w, r)
}

func NewBuilderHTTPMux(base string) *BuilderHTTPMux {
	ret := &BuilderHTTPMux{
		mux: http.NewServeMux(),
	}

	ret.mux.HandleFunc(base+"/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})

	// GET /definition/{hash}
	// Get a definition by hash.
	// if the definition does not exist, return 404.
	ret.mux.HandleFunc("GET "+base+"/definition/{hash}", func(w http.ResponseWriter, r *http.Request) {
		ret.getDefinition(w, r, r.PathValue("hash"))
	})

	// GET /receipt/{hash}
	// Get a receipt by hash.
	ret.mux.HandleFunc("GET "+base+"/receipt/{hash}", func(w http.ResponseWriter, r *http.Request) {
		ret.getReceipt(w, r, r.PathValue("hash"))
	})

	// POST /build/{hash}
	// Build a definition by hash.
	ret.mux.HandleFunc("POST "+base+"/build/{hash}", func(w http.ResponseWriter, r *http.Request) {
		ret.build(w, r, r.PathValue("hash"))
	})

	// GET /output/{hash}/{name}
	// Get an output by hash and name.
	// if the output does not exist, return 404.
	ret.mux.HandleFunc("GET "+base+"/output/{hash}/{name}", func(w http.ResponseWriter, r *http.Request) {
		ret.getOutput(w, r, r.PathValue("hash"), r.PathValue("name"))
	})

	// GET /list
	// Return a JSON list of all definitions and hashes.
	ret.mux.HandleFunc("GET "+base+"/list", func(w http.ResponseWriter, r *http.Request) {
		ret.list(w, r)
	})

	return ret
}
