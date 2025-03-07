package remote

import "net/http"

type ApiKeyHandler struct {
	http.Handler
	ApiKey string
}

// This is not secure to a client inspecting it's own packets but that's not avoidable.
func (h *ApiKeyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Api-Key") != h.ApiKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	h.Handler.ServeHTTP(w, r)
}

type BuilderCreate struct {
	Addr   string
	ApiKey string
}
