package ui

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/database"
	"github.com/tinyrange/tinyrange/pkg/htm"
	"github.com/tinyrange/tinyrange/pkg/htm/bootstrap"
	"github.com/tinyrange/tinyrange/pkg/htm/html"
)

type WebFrontend struct {
	addr               string
	mux                *http.ServeMux
	db                 *database.PackageDatabase
	currentPackageList map[string][]common.PackageQuery
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

	var resultsSection htm.Fragment

	pkgName := r.FormValue("name")

	pkgName = strings.Trim(pkgName, " ")

	if pkgName != "" {
		q, err := common.ParsePackageQuery(pkgName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		results, err := builder.Search(q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var resultRows []htm.Group

		for _, result := range results {
			resultRows = append(resultRows, htm.Group{
				html.Div(
					html.Form(
						html.FormTarget("POST", fmt.Sprintf("/builder/%s/add", name)),
						html.HiddenFormField(html.NewId(), "pkgName", result.Name.Key()),
						bootstrap.SubmitButton("Add", bootstrap.ButtonColorSuccess, bootstrap.ButtonSmall),
					),
				),
				html.Link(fmt.Sprintf("/builder/%s/package/%s", name, result.Name.Key()), html.Text(result.Name.Name)),
				html.Textf("%s", result.Name.Version),
			})
		}

		resultsSection = bootstrap.Card(
			bootstrap.CardTitle("Results"),
			bootstrap.Table(htm.Group{
				html.Textf("Actions"),
				html.Textf("Name"),
				html.Textf("Version"),
			}, resultRows),
		)
	}

	var currentPackagesSection htm.Fragment

	packageList := ui.currentPackageList[name]

	if len(packageList) > 0 {
		var bagRows []htm.Group
		for _, q := range packageList {
			params := make(url.Values)

			params.Add("name", q.String())

			bagRows = append(bagRows, htm.Group{
				html.Link(fmt.Sprintf("/builder/%s?%s", name, params.Encode()), html.Textf("%+v", q.String())),
			})
		}

		currentPackagesSection = bootstrap.Card(
			bootstrap.CardTitle("Package Bag"),
			bootstrap.Table(htm.Group{html.Textf("Name")}, bagRows),
			html.Form(
				html.FormTarget("POST", fmt.Sprintf("/builder/%s/plan", name)),
				bootstrap.FormField("Precompute Dependencies", "precompute", html.FormOptions{
					Kind:          html.FormFieldCheckbox,
					Value:         false,
					LabelSameLine: true,
				}),
				bootstrap.SubmitButton("Make Installation Plan", bootstrap.ButtonColorPrimary),
			),
		)
	}

	buildStatus, err := ui.renderBuildStatus(builder.Packages.Sources...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := htm.Render(r.Context(), w, ui.pageTemplate(
		"Package Metadata Database",
		html.H1(html.Text("Builder: "), html.Link(fmt.Sprintf("/builder/%s", name), html.Textf("%s", builder.DisplayName))),
		bootstrap.Card(
			bootstrap.CardTitle("Search"),
			html.Form(
				html.FormTarget("GET", fmt.Sprintf("/builder/%s", name)),
				bootstrap.FormField("Package Name", "name", html.FormOptions{
					Kind:          html.FormFieldText,
					Value:         pkgName,
					LabelSameLine: true,
				}),
				bootstrap.SubmitButton("Search", bootstrap.ButtonColorPrimary),
			),
		),
		resultsSection,
		currentPackagesSection,
		bootstrap.Card(
			bootstrap.CardTitle("Internal Build Status"),
			buildStatus,
		),
	)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ui *WebFrontend) handleBuilderAddPackage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	builder, ok := ui.db.ContainerBuilders[name]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	pkgName := r.FormValue("pkgName")

	pkg, ok := builder.Get(pkgName)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	q := pkg.Name.Query()

	for _, other := range ui.currentPackageList[builder.Name] {
		if other == q {
			http.Redirect(w, r, fmt.Sprintf("/builder/%s", name), http.StatusFound)

			return
		}
	}

	ui.currentPackageList[builder.Name] = append(ui.currentPackageList[builder.Name], q)

	http.Redirect(w, r, fmt.Sprintf("/builder/%s", name), http.StatusFound)
}

func (ui *WebFrontend) handleBuilderMakePlan(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	builder, ok := ui.db.ContainerBuilders[name]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	precompute := r.FormValue("precompute") != ""

	level := "level1"
	if precompute {
		level = "level2"
	}

	pkgs := ui.currentPackageList[builder.Name]

	plan, err := builder.Plan(ui.db, pkgs, common.TagList{level}, common.PlanOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var directives []htm.Group

	for _, directive := range plan.Directives() {
		directives = append(directives, htm.Group{
			html.Code(html.Textf("%+v", directive)),
		})
	}

	dFile, err := database.EmitDockerfile(plan)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := htm.Render(r.Context(), w, ui.pageTemplate(
		"Package Metadata Database",
		html.H1(html.Text("Builder: "), html.Link(fmt.Sprintf("/builder/%s", name), html.Textf("%s", builder.DisplayName))),
		bootstrap.Card(
			bootstrap.CardTitle("Plan Directives"),
			bootstrap.Table(htm.Group{}, directives),
		),
		bootstrap.Card(
			bootstrap.CardTitle("Rendered Dockerfile"),
			html.Code(html.Pre(html.Textf("%s", dFile))),
		),
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

	// var installers htm.Group

	// for _, installer := range pkg.Installers {
	// 	var depends []htm.Group

	// 	for _, depend := range installer.Dependencies {
	// 		params := make(url.Values)

	// 		params.Add("name", depend.String())

	// 		depends = append(depends, htm.Group{
	// 			html.Link(fmt.Sprintf("/builder/%s?%s", name, params.Encode()), html.Textf("%+v", depend.String())),
	// 		})
	// 	}

	// 	var directives []htm.Group

	// 	for _, directive := range installer.Directives {
	// 		directives = append(directives, htm.Group{
	// 			html.Code(html.Textf("%+v", directive)),
	// 		})
	// 	}

	// 	installers = append(installers, bootstrap.Card(
	// 		html.H5(html.Textf("Tags: %+v", installer.Tags)),
	// 		html.H6(html.Textf("Depends")),
	// 		bootstrap.Table(htm.Group{html.Textf("Name")}, depends),
	// 		html.H6(html.Textf("Directives")),
	// 		bootstrap.Table(htm.Group{}, directives),
	// 	))
	// }

	// buf := new(bytes.Buffer)

	// if pkg.Raw != "" {
	// 	if err := json.Indent(buf, []byte(pkg.Raw), "", "  "); err != nil {
	// 		http.Error(w, err.Error(), http.StatusInternalServerError)
	// 		return
	// 	}
	// }

	if err := htm.Render(r.Context(), w, ui.pageTemplate(
		"Package Metadata Database",
		html.H1(html.Text("Builder: "), html.Link(fmt.Sprintf("/builder/%s", name), html.Textf("%s", builder.DisplayName))),
		bootstrap.Card(bootstrap.CardTitle(pkg.Name.String())),
		// bootstrap.Card(
		// 	bootstrap.CardTitle("Installers"),
		// 	installers,
		// ),
		// bootstrap.Card(
		// 	bootstrap.CardTitle("Raw"),
		// 	html.Code(html.Pre(html.Textf("%s", buf.String()))),
		// ),
	)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ui *WebFrontend) registerRoutes() {
	ui.mux.HandleFunc("/", ui.handleIndex)
	ui.mux.HandleFunc("/builder/{name}", ui.handleBuilderIndex)
	ui.mux.HandleFunc("/builder/{name}/add", ui.handleBuilderAddPackage)
	ui.mux.HandleFunc("/builder/{name}/plan", ui.handleBuilderMakePlan)
	ui.mux.HandleFunc("/builder/{name}/package/{pkgName}", ui.handleBuilderPackageDetail)
}

func (ui *WebFrontend) ListenAndServe() error {
	ui.registerRoutes()

	slog.Info("listening", "on", "http://"+ui.addr)
	return http.ListenAndServe(ui.addr, ui.mux)
}

func New(addr string, db *database.PackageDatabase) *WebFrontend {
	return &WebFrontend{
		addr:               addr,
		mux:                http.NewServeMux(),
		db:                 db,
		currentPackageList: make(map[string][]common.PackageQuery),
	}
}
