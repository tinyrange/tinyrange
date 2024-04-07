package ui

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tinyrange/pkg2/db"
	"github.com/tinyrange/pkg2/htm"
	"github.com/tinyrange/pkg2/htm/bootstrap"
	"github.com/tinyrange/pkg2/htm/html"
)

func pageTemplate(name db.PackageName, start time.Time, frags ...htm.Fragment) htm.Fragment {
	return html.Html(
		html.Head(
			bootstrap.CSSSrc,
			bootstrap.JavaScriptSrc,
			bootstrap.ColorPickerSrc,
			html.Style(".card {margin-top: 10px; }"),
		),
		html.Body(
			bootstrap.Navbar(
				bootstrap.NavbarBrand("/", html.Text("Package Metadata Search")),
			),
			html.Div(
				bootstrap.Container,
				bootstrap.Card(
					bootstrap.CardTitle("WARNING"),
					html.Text("This software is in beta. Packages are not necessarily up to date and dependency resolution will have errors."),
				),
				bootstrap.Card(
					bootstrap.CardTitle("Search"),
					html.Form(
						html.FormTarget("GET", "/search"),
						bootstrap.FormField("Distribution", "distribution", html.FormOptions{
							Kind:          html.FormFieldText,
							Value:         name.Distribution,
							LabelSameLine: true,
						}),
						bootstrap.FormField("Package Name", "name", html.FormOptions{
							Kind:          html.FormFieldText,
							Value:         name.Name,
							LabelSameLine: true,
						}),
						bootstrap.FormField("Package Version", "version", html.FormOptions{
							Kind:          html.FormFieldText,
							Value:         name.Version,
							LabelSameLine: true,
						}),
						bootstrap.FormField("Package Architecture", "architecture", html.FormOptions{
							Kind:          html.FormFieldText,
							Value:         name.Architecture,
							LabelSameLine: true,
						}),
						bootstrap.SubmitButton("Search", bootstrap.ButtonColorPrimary),
					),
				),
				htm.Group(frags),
				html.Div(html.Textf("Took %s to generate page.", time.Since(start))),
			),
		),
	)
}

func RegisterHandlers(pkgDb *db.PackageDatabase, mux *http.ServeMux) {
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		r.ParseForm()

		distro := strings.Trim(r.FormValue("distribution"), " ")
		name := strings.Trim(r.FormValue("name"), " ")
		version := strings.Trim(r.FormValue("version"), " ")
		architecture := strings.Trim(r.FormValue("architecture"), " ")

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

		var rows []htm.Group

		for _, pkg := range results {
			rows = append(rows, htm.Group{
				html.Textf("%s", pkg.Name.Distribution),
				html.Link("/info?key="+url.QueryEscape(pkg.Id()), html.Textf(pkg.Name.Name)),
				html.Textf("%s", pkg.Name.Version),
				html.Textf("%s", pkg.Name.Architecture),
			})
		}

		page := pageTemplate(search, start, bootstrap.Table(htm.Group{
			html.Textf("Distribution"),
			html.Textf("Name"),
			html.Textf("Version"),
			html.Textf("Architecture"),
		}, rows))

		if err := htm.Render(r.Context(), w, page); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		r.ParseForm()

		key := r.FormValue("key")

		pkg, ok := pkgDb.Get(key)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var dependList []htm.Group
		for _, optionList := range pkg.Depends {
			for _, option := range optionList {
				dependList = append(dependList, htm.Group{
					html.Textf("%s", option.Distribution),
					html.Link("/search?"+option.UrlParams(), html.Textf("%s", option.Name)),
					html.Textf("%s", pkg.Name.Version),
					html.Textf("%s", pkg.Name.Architecture),
				})
			}
		}

		var aliasList []htm.Group
		for _, alias := range pkg.Aliases {
			aliasList = append(aliasList, htm.Group{
				html.Textf("%s", alias.Distribution),
				html.Textf("%s", alias.Name),
				html.Textf("%s", pkg.Name.Version),
				html.Textf("%s", pkg.Name.Architecture),
			})
		}

		var downloadUrls []htm.Group
		for _, downloadUrl := range pkg.DownloadUrls {
			downloadUrls = append(downloadUrls, htm.Group{
				html.Link(downloadUrl, html.Textf("%s", downloadUrl)),
			})
		}

		page := pageTemplate(pkg.Name, start,
			bootstrap.Card(
				html.H4(html.Textf("%s @ %s", pkg.Name.Name, pkg.Name.Version)),
				html.H5(html.Textf("License: %s", pkg.License)),
				html.H5(html.Text("Description")),
				html.Pre(html.Text(pkg.Description)),
			),
			bootstrap.Card(
				bootstrap.CardTitle("Download URLs"),
				bootstrap.Table(htm.Group{
					html.Textf("URL"),
				}, downloadUrls),
			),
			bootstrap.Card(
				bootstrap.CardTitle("Depends"),
				bootstrap.Table(htm.Group{
					html.Textf("Distribution"),
					html.Textf("Name"),
					html.Textf("Version"),
					html.Textf("Architecture"),
				}, dependList),
			),
			bootstrap.Card(
				bootstrap.CardTitle("Aliases"),
				bootstrap.Table(htm.Group{
					html.Textf("Distribution"),
					html.Textf("Name"),
					html.Textf("Version"),
					html.Textf("Architecture"),
				}, aliasList),
			),
		)

		if err := htm.Render(r.Context(), w, page); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		page := pageTemplate(db.PackageName{}, start)

		if err := htm.Render(r.Context(), w, page); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}
