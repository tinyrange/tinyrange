package ui

import (
	"embed"
	"html/template"
	"net/http"

	"github.com/tinyrange/pkg2/db"
)

//go:embed templates/*.html
var templateFs embed.FS

var templates = template.Must(template.ParseFS(templateFs, "templates/*.html"))

func RegisterHandlers(pkgDb *db.PackageDatabase, mux *http.ServeMux) {
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()

		distro := r.FormValue("distribution")
		name := r.FormValue("name")
		version := r.FormValue("version")
		architecture := r.FormValue("architecture")

		search := db.PackageName{
			Distribution: distro,
			Name:         name,
			Version:      version,
			Architecture: architecture,
		}

		results, err := pkgDb.Search(search, 50)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := templates.ExecuteTemplate(w, "results.html", results); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()

		key := r.FormValue("key")

		pkg, ok := pkgDb.Get(key)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if err := templates.ExecuteTemplate(w, "info.html", pkg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := templates.ExecuteTemplate(w, "index.html", nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}
