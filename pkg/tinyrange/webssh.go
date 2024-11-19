package tinyrange

import (
	"embed"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/tinyrange/tinyrange/pkg/browser/browser"
	"github.com/tinyrange/tinyrange/pkg/htm"
	"github.com/tinyrange/tinyrange/pkg/htm/bootstrap"
	"github.com/tinyrange/tinyrange/pkg/htm/html"
	"github.com/tinyrange/tinyrange/pkg/netstack"
)

//go:embed ssh_static/*
var ssh_static embed.FS

//go:embed ssh_terminal.js
var sshJsRaw string

var SSH_JS = html.JavaScript(sshJsRaw)

var SSH_CSS = html.Style(`
#terminal {
	min-height: 300px;
	max-height: 50vh;
}
div.fillScreen {
	position: fixed;
	top: 0;
	left: 0;
	max-height: 100vh !important;
	z-index: 100;
}

button.fillScreen {
	position: fixed;
	bottom: 20px;
	right: 20px;
	z-index: 101;
}`)

func renderPage(minimal bool) htm.Fragment {
	head := html.Head(
		html.MetaCharset("UTF-8"),
		html.Title("TinyRange"),
		html.MetaViewport("width=device-width, initial-scale=1"),
		bootstrap.CSSSrc,
		bootstrap.JavaScriptSrc,
		bootstrap.ColorPickerSrc,
		html.Style(`.card {
margin-bottom: 1rem;
}`),
	)

	interaction := htm.Group{
		html.JavaScriptSrc("./ssh_static/xterm.min.js"),
		html.LinkCSS("./ssh_static/xterm.css"),
		html.JavaScriptSrc("./ssh_static/xterm-addon-fit.min.js"),
		bootstrap.Button(bootstrap.ButtonColorDark, html.Text("Toggle Fill Screen"), html.Id("fillScreen")),
		html.Div(html.Id("terminal")),
		SSH_CSS,
		SSH_JS,
	}

	if minimal {
		return html.Html(
			htm.Attr("lang", "en"),
			head,
			html.Body(
				html.Div(bootstrap.ContainerFluid, interaction),
				html.JavaScript(`document.addEventListener("DOMContentLoaded", () => toggleFill());`),
			),
		)
	} else {
		return html.Html(
			htm.Attr("lang", "en"),
			head,
			html.Body(
				bootstrap.Navbar(
					bootstrap.NavbarBrand("/", html.Text("TinyRange")),
				),
				html.Div(bootstrap.Container, interaction),
			),
		)
	}
}

var upgrader = websocket.Upgrader{}

func runWebSsh(ns *netstack.NetStack, address string, username string, secureSSH SecureSSHConfig, args string) error {
	host, arg, _ := strings.Cut(args, ",")

	minimal := arg == "minimal"

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := htm.Render(r.Context(), w, renderPage(minimal)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.Handle("/ssh_static/", http.FileServer(http.FS(ssh_static)))

	mux.HandleFunc("/spawn", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Warn("failed to upgrade SSH connection", "error", err)
			return
		}

		if err := newWebSocketSSH(ws, ns, address, username, secureSSH); err != nil {
			slog.Warn("failed to create SSH connection", "error", err)
			return
		}
	})

	listener, err := net.Listen("tcp", host)
	if err != nil {
		return err
	}

	if arg == "nobrowser" || arg == "minimal" {

	} else {
		if err := browser.Open("http://" + listener.Addr().String()); err != nil {
			return err
		}
	}

	slog.Info("listening", "address", "http://"+listener.Addr().String())

	return http.Serve(listener, mux)
}
