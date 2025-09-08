package http

import (
	"net/http"

	"github.com/tinyrange/tinyrange/build/internal/common"
)

func New(db common.Database) http.Handler {
	mux := http.NewServeMux()
	return mux
}
