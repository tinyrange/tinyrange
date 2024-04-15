package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tinyrange/pkg2/db"
	"github.com/tinyrange/pkg2/htm"
	"github.com/tinyrange/pkg2/htm/bootstrap"
	"github.com/tinyrange/pkg2/htm/html"
)

func pageTemplate(pkgDb *db.PackageDatabase, name db.PackageName, start time.Time, frags ...htm.Fragment) htm.Fragment {
	distributions := pkgDb.DistributionList()
	architectures := pkgDb.ArchitectureList()

	return html.Html(
		htm.Attr("lang", "en"),
		html.Head(
			html.MetaCharset("UTF-8"),
			html.Title("Package Metadata Search: Alpha"),
			html.MetaViewport("width=device-width, initial-scale=1"),
			bootstrap.CSSSrc,
			bootstrap.JavaScriptSrc,
			bootstrap.ColorPickerSrc,
			html.Style(".card {margin-top: 10px; }"),
		),
		html.Body(
			bootstrap.Navbar(
				bootstrap.NavbarBrand("/", html.Text("Package Metadata Search: Alpha")),
			),
			html.Div(
				bootstrap.Container,
				bootstrap.Card(
					bootstrap.CardTitle("WARNING"),
					html.Div(
						html.Text("This software is in alpha. Packages are not necessarily up to date and dependency resolution will have errors. Any questions please open a issue at "),
						html.Link("https://github.com/tinyrange/pkg2", html.Text("https://github.com/tinyrange/pkg2")),
						html.Text(" or send an email to "),
						html.Link("mailto:joshua@jscarsbrook.me", html.Text("joshua@jscarsbrook.me")),
						html.Text("."),
					),
				),
				bootstrap.Card(
					bootstrap.CardTitle("Search"),
					html.Form(
						html.FormTarget("GET", "/search"),
						bootstrap.FormField("Distribution", "distribution", html.FormOptions{
							Kind:          html.FormFieldSelect,
							Value:         name.Distribution,
							LabelSameLine: true,
							Options:       distributions,
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
							Kind:          html.FormFieldSelect,
							Value:         name.Architecture,
							LabelSameLine: true,
							Options:       architectures,
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

		distribution := strings.Trim(r.FormValue("distribution"), " \t")
		name := strings.Trim(r.FormValue("name"), " \t")
		version := strings.Trim(r.FormValue("version"), " \t")
		architecture := strings.Trim(r.FormValue("architecture"), " \t")

		search, err := db.NewPackageName(distribution, name, version, architecture)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
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

		page := pageTemplate(pkgDb, search, start, bootstrap.Table(htm.Group{
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
					html.Textf("%s", option.Version),
					html.Textf("%s", option.Architecture),
				})
			}
		}

		var aliasList []htm.Group
		for _, alias := range pkg.Aliases {
			aliasList = append(aliasList, htm.Group{
				html.Textf("%s", alias.Distribution),
				html.Textf("%s", alias.Name),
				html.Textf("%s", alias.Version),
				html.Textf("%s", alias.Architecture),
			})
		}

		var downloadUrls []htm.Group
		for _, downloadUrl := range pkg.DownloadUrls {
			downloadUrls = append(downloadUrls, htm.Group{
				html.Link(downloadUrl, html.Textf("%s", downloadUrl)),
			})
		}

		var builders htm.Group
		for _, builder := range pkg.Builders {
			contents, err := json.MarshalIndent(builder, "", "  ")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			builders = append(builders, bootstrap.Card(
				bootstrap.CardTitle("Builder: "+builder.Name.String()),
				html.Code(html.Pre(html.Text(string(contents)))),
			))
		}

		page := pageTemplate(pkgDb, pkg.Name, start,
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
			builders,
		)

		if err := htm.Render(r.Context(), w, page); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		r.ParseForm()

		key := r.FormValue("key")

		if key != "" {
			fetcher, err := pkgDb.GetFetcher(key)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			var logMessages []string
			for _, msg := range fetcher.Messages {
				logMessages = append(logMessages, msg.String())
			}

			var rows []htm.Group

			for _, pkg := range fetcher.Packages {
				rows = append(rows, htm.Group{
					html.Textf("%s", pkg.Name.Distribution),
					html.Link("/info?key="+url.QueryEscape(pkg.Id()), html.Textf(pkg.Name.Name)),
					html.Textf("%s", pkg.Name.Version),
					html.Textf("%s", pkg.Name.Architecture),
				})
				if len(rows) > 100 {
					break
				}
			}

			page := pageTemplate(pkgDb, db.PackageName{}, start,
				bootstrap.Card(
					bootstrap.CardTitle(fetcher.String()),
					html.Div(html.Textf("Last Updated: %s", fetcher.LastUpdated)),
					html.Div(html.Textf("Last Update Time: %s", fetcher.LastUpdateTime)),
					html.Div(html.Textf("Package Count: %d", len(fetcher.Packages))),
				),
				bootstrap.Card(
					bootstrap.CardTitle("Log"),
					html.Pre(html.Code(html.Text(strings.Join(logMessages, "\n")))),
				),
				bootstrap.Card(
					bootstrap.CardTitle("Packages (First 100)"),
					bootstrap.Table(htm.Group{
						html.Textf("Distribution"),
						html.Textf("Name"),
						html.Textf("Version"),
						html.Textf("Architecture"),
					}, rows),
				),
			)

			if err := htm.Render(r.Context(), w, page); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			var rows []htm.Group

			status, err := pkgDb.FetcherStatus()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			for _, row := range status {
				rows = append(rows, htm.Group{
					html.Link(fmt.Sprintf("/status?key=%s", row.Key), html.Code(html.Textf("%s", row.Key[:10]))),
					html.Code(html.Textf("%s...", row.Name[:min(len(row.Name), 60)])),
					html.Textf("%s", row.Status.String()),
					html.Textf("%s", row.LastUpdateTime.String()),
					html.Textf("%s", row.LastUpdated.Format(time.UnixDate)),
					html.Textf("%d", row.PackageCount),
				})
			}

			page := pageTemplate(pkgDb, db.PackageName{}, start,
				bootstrap.Card(
					bootstrap.CardTitle("Fetcher Status"),
					bootstrap.Table(htm.Group{
						html.Text("Key"),
						html.Text("Name"),
						html.Text("Status"),
						html.Text("Last Update Time"),
						html.Text("Last Updated"),
						html.Text("Count"),
					}, rows),
				),
			)

			if err := htm.Render(r.Context(), w, page); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		page := pageTemplate(pkgDb, db.PackageName{}, start, bootstrap.Card(
			bootstrap.CardTitle("Package Metadata Search: Alpha"),
			html.Div(html.Textf("Currently Indexing: %d Packages", pkgDb.Count())),
		))

		if err := htm.Render(r.Context(), w, page); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}
