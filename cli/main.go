package cli

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/tinyrange/tinyrange/build"
	"github.com/tinyrange/tinyrange/build/cache/memory"

	buildhttp "github.com/tinyrange/tinyrange/build/http"
)

var (
	serverAddr         string
	serverStaticPath   string
	serverApiOnly      bool
	serverInsecureCors bool
)

func newDb() (build.Database, error) {
	return build.NewDatabase(memory.NewCache())
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run the TinyRange build server",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := newDb()
		if err != nil {
			return err
		}

		var mux http.Handler
		if serverApiOnly {
			mux = buildhttp.New(db, "")
		} else {
			handler := buildhttp.New(db, "/api")

			serveMux := http.NewServeMux()

			if serverStaticPath != "" {
				slog.Info("serving static files", "path", serverStaticPath)
				fs := http.FileServer(http.Dir(serverStaticPath))
				serveMux.Handle("/", fs)
			} else {
				slog.Info("no static files configured")
			}

			serveMux.Handle("/api/", handler)

			mux = serveMux
		}

		if serverInsecureCors {
			oldMux := mux
			mux = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusNoContent)
					return
				}

				oldMux.ServeHTTP(w, r)
			})
		}

		listen, err := net.Listen("tcp", serverAddr)
		if err != nil {
			return err
		}
		defer listen.Close()

		slog.Info("listening", "addr", listen.Addr().String())

		srv := &http.Server{
			Handler: mux,
		}

		return srv.Serve(listen)
	},
}

var rootCmd = &cobra.Command{
	Use:   "tinyrange",
	Short: "TinyRange is a small, self-hosted range server",
}

func init() {
	serverCmd.Flags().StringVar(&serverAddr, "addr", ":8080", "Address to listen on")
	serverCmd.Flags().StringVar(&serverStaticPath, "static", "", "Path to static files")
	serverCmd.Flags().BoolVar(&serverApiOnly, "api-only", false, "Only expose the API")
	serverCmd.Flags().BoolVar(&serverInsecureCors, "insecure-cors", false, "Enable insecure CORS")

	rootCmd.AddCommand(serverCmd)
}

func Main() error {
	return rootCmd.Execute()
}
