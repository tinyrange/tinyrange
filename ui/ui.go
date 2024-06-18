package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

	"github.com/tinyrange/pkg2/v2/common"
	"github.com/tinyrange/pkg2/v2/database"
	"github.com/tinyrange/pkg2/v2/htm"
	"github.com/tinyrange/pkg2/v2/htm/bootstrap"
	"github.com/tinyrange/pkg2/v2/htm/html"
)

type WebFrontend struct {
	addr string
	mux  *http.ServeMux
	db   *database.PackageDatabase
}

func (ui *WebFrontend) renderBuildStatus(defs ...common.BuildDefinition) (htm.Fragment, error) {
	var ret htm.Group

	for _, def := range defs {
		status, err := ui.db.GetBuildStatus(def)
		if err != nil {
			return nil, err
		}

		children, err := ui.renderBuildStatus(status.Children...)
		if err != nil {
			return nil, err
		}

		ret = append(ret, bootstrap.Card(
			html.Div(html.Code(html.Textf("%s", status.Tag))),
			html.Div(html.Textf("%s", status.Status)),
			children,
		))
	}

	return ret, nil
}

func (ui *WebFrontend) pageTemplate(title string, body ...htm.Fragment) htm.Fragment {
	return html.Html(
		htm.Attr("lang", "en"),
		html.Head(
			html.MetaCharset("UTF-8"),
			html.Title(title),
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
				htm.Group(body),
			),
		),
	)
}

func (ui *WebFrontend) handleIndex(w http.ResponseWriter, r *http.Request) {
	var rows []htm.Group

	var keys []string
	for k := range ui.db.ContainerBuilders {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	for _, k := range keys {
		v := ui.db.ContainerBuilders[k]
		rows = append(rows, htm.Group{
			html.Link(fmt.Sprintf("/builder/%s", k), html.Textf("%s", v.DisplayName)),
		})
	}

	if err := htm.Render(r.Context(), w, ui.pageTemplate(
		"Package Metadata Database",
		bootstrap.Table(htm.Group{
			html.Textf("Name"),
		}, rows),
	)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ui *WebFrontend) handleBuilderIndex(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	builder, ok := ui.db.ContainerBuilders[name]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	buildStatus, err := ui.renderBuildStatus(builder.Packages.Sources...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := htm.Render(r.Context(), w, ui.pageTemplate(
		"Package Metadata Database",
		html.H1(html.Textf("Builder: %s", builder.DisplayName)),
		bootstrap.Card(
			bootstrap.CardTitle("Search"),
			html.Form(
				html.FormTarget("GET", fmt.Sprintf("/builder/%s/search", name)),
				bootstrap.FormField("Package Name", "name", html.FormOptions{
					Kind:          html.FormFieldText,
					Value:         "",
					LabelSameLine: true,
				}),
				bootstrap.SubmitButton("Search", bootstrap.ButtonColorPrimary),
			),
		),
		bootstrap.Card(
			bootstrap.CardTitle("Build Status"),
			buildStatus,
		),
	)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ui *WebFrontend) handleBuilderSearch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	builder, ok := ui.db.ContainerBuilders[name]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	q := common.PackageQuery{
		Name: r.FormValue("name"),
	}

	results, err := builder.Search(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resultRows []htm.Group

	for _, result := range results {
		resultRows = append(resultRows, htm.Group{
			html.Link(fmt.Sprintf("/builder/%s/package/%s", name, result.Name.Key()), html.Text(result.Name.Name)),
			html.Textf("%s", result.Name.Version),
		})
	}

	if err := htm.Render(r.Context(), w, ui.pageTemplate(
		"Package Metadata Database",
		bootstrap.Table(htm.Group{
			html.Textf("Name"),
			html.Textf("Version"),
		}, resultRows),
	)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ui *WebFrontend) handleBuilderPackageDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	builder, ok := ui.db.ContainerBuilders[name]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	pkgName := r.PathValue("pkgName")

	pkg, ok := builder.Get(pkgName)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	buf := new(bytes.Buffer)

	if err := json.Indent(buf, []byte(pkg.Raw), "", "  "); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := htm.Render(r.Context(), w, ui.pageTemplate(
		"Package Metadata Database",
		bootstrap.Card(bootstrap.CardTitle(pkg.Name.String())),
		bootstrap.Card(
			bootstrap.CardTitle("Raw"),
			html.Code(html.Pre(html.Textf("%s", buf.String()))),
		),
	)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ui *WebFrontend) registerRoutes() {
	ui.mux.HandleFunc("/", ui.handleIndex)
	ui.mux.HandleFunc("/builder/{name}", ui.handleBuilderIndex)
	ui.mux.HandleFunc("/builder/{name}/search", ui.handleBuilderSearch)
	ui.mux.HandleFunc("/builder/{name}/package/{pkgName}", ui.handleBuilderPackageDetail)
}

func (ui *WebFrontend) ListenAndServe() error {
	ui.registerRoutes()

	slog.Info("listening", "on", "http://"+ui.addr)
	return http.ListenAndServe(ui.addr, ui.mux)
}

func New(addr string, db *database.PackageDatabase) *WebFrontend {
	return &WebFrontend{
		addr: addr,
		mux:  http.NewServeMux(),
		db:   db,
	}
}
