package trweb

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/agnivade/levenshtein"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/htm"
	"github.com/tinyrange/tinyrange/pkg/htm/bootstrap"
	"github.com/tinyrange/tinyrange/pkg/htm/html"
	"github.com/tinyrange/tinyrange/pkg/htm/htmx"
	"github.com/tinyrange/tinyrange/pkg/login"
)

type WebApplication struct {
	mux           *http.ServeMux
	db            common.PackageDatabase
	webSshAddress string
	runningCmd    *exec.Cmd
}

func (app *WebApplication) pageLayout(body ...htm.Fragment) htm.Fragment {
	return html.Html(
		htm.Attr("lang", "en"),
		html.Head(
			html.MetaCharset("UTF-8"),
			html.Title("TinyRange"),
			html.MetaViewport("width=device-width, initial-scale=1"),
			bootstrap.CSSSrc,
			bootstrap.JavaScriptSrc,
			bootstrap.ColorPickerSrc,
			htmx.JavaScriptSrc,
			html.Style(`iframe {
				width: 100%;
				height: 500px;
			}
			.pad {
				padding: 0.5rem;
			}`),
		),
		html.Body(
			bootstrap.Navbar(
				bootstrap.NavbarBrand("/", html.Text("TinyRange")),
			),
			html.Div(bootstrap.Container, htm.Group(body)),
		),
	)
}

func (app *WebApplication) serveFragment(w http.ResponseWriter, r *http.Request, fragment htm.Fragment) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	htm.Render(r.Context(), w, fragment)
}

func (app *WebApplication) serveIndex(w http.ResponseWriter, r *http.Request) {
	if app.runningCmd != nil {
		http.Redirect(w, r, "/run", http.StatusFound)
		return
	}

	app.serveFragment(w, r, app.pageLayout(
		html.Form(
			html.Id("start-form"),
			html.FormTarget("POST", "/start"),
			bootstrap.FormField("Builder", "builder", html.FormOptions{
				Kind:    html.FormFieldSelect,
				Options: []string{"alpine@3.20"},
				Value:   "alpine@3.20",
			}),
			html.Div(html.Id("package_list")),
			bootstrap.FormField("Add Package", "query",
				html.FormOptions{
					Kind:        html.FormFieldText,
					Placeholder: "Search Query",
					Value:       "",
				},
				htmx.Get("/package_results"),
				htmx.Trigger(htmx.EventKeyup, htmx.ModifierChanged, htmx.ModifierDelay(250*time.Millisecond)),
				htmx.Include(htmx.FormName("builder")),
				htmx.Include("#package_list"),
				htmx.Target("results"),
			),
			html.Div(html.Id("results")),
			bootstrap.SubmitButton("Start", bootstrap.ButtonColorPrimary),
		),
	))
}

func (app *WebApplication) serveRun(w http.ResponseWriter, r *http.Request) {
	if app.runningCmd == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	app.serveFragment(w, r, app.pageLayout(
		html.Form(
			html.FormTarget("POST", "/stop"),
			bootstrap.SubmitButton("Stop", bootstrap.ButtonColorDanger),
		),
		htm.NewHtmlFragment("iframe", htm.Attr("src", "http://"+app.webSshAddress)),
	))
}

func (app *WebApplication) runTemplate(filename string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	app.runningCmd = exec.Command(exe, "run-vm", filename)

	if err := app.runningCmd.Start(); err != nil {
		return err
	}

	return nil
}

func (app *WebApplication) getConfig(r *http.Request) (login.Config, error) {
	config := login.Config{
		Version:     login.CURRENT_CONFIG_VERSION,
		Builder:     "alpine@3.20",
		CpuCores:    1,
		MemorySize:  1024,
		StorageSize: 1024,
		WebSSH:      fmt.Sprintf("%s,minimal", app.webSshAddress),
	}

	addPackages := r.Form["add_package"]

	if len(addPackages) > 0 {
		config.Packages = addPackages
	}

	return config, nil
}

func (app *WebApplication) handleStart(w http.ResponseWriter, r *http.Request) {
	// parse the form.
	if err := r.ParseForm(); err != nil {
		slog.Error("Failed to parse form", "error", err)
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	config, err := app.getConfig(r)
	if err != nil {
		slog.Error("Failed to get config", "error", err)
		http.Error(w, "Failed to get config", http.StatusInternalServerError)
		return
	}

	templateFilename, err := config.MakeTemplate(app.db)
	if err != nil {
		slog.Error("Failed to get template filename", "error", err)
		http.Error(w, "Failed to get template filename", http.StatusInternalServerError)
		return
	}

	slog.Info("running template", "filename", templateFilename)

	if err := app.runTemplate(templateFilename); err != nil {
		slog.Error("Failed to run template", "error", err)
		http.Error(w, "Failed to run template", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/run", http.StatusFound)
}

func (app *WebApplication) handleStop(w http.ResponseWriter, r *http.Request) {
	if app.runningCmd != nil {
		if err := app.runningCmd.Process.Kill(); err != nil {
			slog.Error("Failed to kill process", "error", err)
			http.Error(w, "Failed to kill process", http.StatusInternalServerError)
			return
		}
		app.runningCmd = nil
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (app *WebApplication) handlePackageResults(w http.ResponseWriter, r *http.Request) {
	builder := r.URL.Query().Get("builder")
	query := r.URL.Query().Get("query")
	existing := r.URL.Query()["add_package"]

	if builder == "" {
		http.Error(w, "Missing builder", http.StatusBadRequest)
		return
	}

	if query == "" {
		// return nothing
		app.serveFragment(w, r, htm.Group{})
		return
	}

	ctx := app.db.NewBuildContext(nil)

	b, err := app.db.GetContainerBuilder(ctx, builder, config.HostArchitecture)
	if err != nil {
		slog.Error("Failed to get container builder", "error", err)
		http.Error(w, "Failed to get container builder", http.StatusInternalServerError)
		return
	}

	q, err := common.ParsePackageQuery(query)
	if err != nil {
		slog.Error("Failed to parse package query", "error", err)
		http.Error(w, "Failed to parse package query", http.StatusInternalServerError)
		return
	}

	q.MatchDirect = true
	q.MatchPartialName = true

	results, err := b.Search(q)
	if err != nil {
		slog.Error("Failed to search", "error", err)
		http.Error(w, "Failed to search", http.StatusInternalServerError)
		return
	}

	if len(results) == 0 {
		app.serveFragment(w, r, bootstrap.Alert(bootstrap.AlertColorWarning, html.Text("No results found")))
		return
	}

	existingMap := make(map[string]struct{})

	for _, pkg := range existing {
		existingMap[pkg] = struct{}{}
	}

	var resultStrings []string

	for _, result := range results {
		if _, ok := existingMap[result.Name.String()]; ok {
			continue
		}

		resultStrings = append(resultStrings, result.Name.String())
	}

	// sort using levenshtein distance
	if len(resultStrings) > 1 {
		slices.SortFunc(resultStrings, func(a, b string) int {
			return levenshtein.ComputeDistance(a, query) - levenshtein.ComputeDistance(b, query)
		})
	}

	var rendered htm.Group
	for _, result := range resultStrings {
		if len(rendered) > 20 {
			break
		}

		id := html.NewId()

		rendered = append(rendered, bootstrap.Card(
			html.Span(htm.Class("pad"), html.Code(html.Text(result))),
			html.Form(
				id,
				html.HiddenFormField("", "query", result),
				bootstrap.LinkButton("#", bootstrap.ButtonColorSuccess, bootstrap.ButtonSmall,
					html.Text("Add"),
					htmx.Get("/add_package"),
					htmx.Include("#"+string(id), htmx.FormName("builder"), "#package_list"),
					htmx.Target("package_list"),
				),
			),
		))
	}

	app.serveFragment(w, r, htm.Group{
		rendered,
	})
}

func (app *WebApplication) handleAddPackage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	existing := r.URL.Query()["add_package"]

	if query == "" {
		http.Error(w, "Missing query", http.StatusBadRequest)
		return
	}

	var packageList htm.Group

	for _, pkg := range append(existing, query) {
		q, err := common.ParsePackageQuery(pkg)
		if err != nil {
			slog.Error("Failed to parse package query", "error", err)
			http.Error(w, "Failed to parse package query", http.StatusInternalServerError)
			return
		}

		packageList = append(packageList, bootstrap.Card(
			html.Span(htm.Class("pad"), html.Code(html.Text(q.Name))),
			html.Span(htm.Class("pad"), html.Code(html.Text(q.Version))),
			html.HiddenFormField("", "add_package", pkg),
		))
	}

	app.serveFragment(w, r, packageList)
}

func (app *WebApplication) Run(listen string) error {
	app.mux.HandleFunc("GET /", app.serveIndex)
	app.mux.HandleFunc("GET /run", app.serveRun)
	app.mux.HandleFunc("POST /start", app.handleStart)
	app.mux.HandleFunc("POST /stop", app.handleStop)
	app.mux.HandleFunc("GET /package_results", app.handlePackageResults)
	app.mux.HandleFunc("GET /add_package", app.handleAddPackage)

	slog.Info("Listening", "listen", "http://"+listen)

	return http.ListenAndServe(listen, app.mux)
}

func New(db common.PackageDatabase) *WebApplication {
	return &WebApplication{
		db:            db,
		mux:           http.NewServeMux(),
		webSshAddress: "127.0.0.1:5124",
	}
}
