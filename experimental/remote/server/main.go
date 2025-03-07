package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"github.com/tinyrange/tinyrange/experimental/remote"
)

var (
	connectAddr = flag.String("connect", "", "address to connect to")
)

func appMain() error {
	flag.Parse()

	if *connectAddr == "" {
		return fmt.Errorf("connect address is required")
	}

	apiKey, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	serverApiKey := uuid.NewString()

	mux := http.NewServeMux()

	mux.HandleFunc("/buildinfo", func(w http.ResponseWriter, r *http.Request) {
		info, ok := debug.ReadBuildInfo()
		if !ok {
			http.Error(w, "no build info", http.StatusInternalServerError)
			return
		}

		if err := json.NewEncoder(w).Encode(info); err != nil {
			http.Error(w, "failed to encode", http.StatusInternalServerError)
			return
		}
	})

	go http.Serve(listen, &remote.ApiKeyHandler{
		Handler: mux,
		ApiKey:  serverApiKey,
	})

	addr := listen.Addr().(*net.TCPAddr)

	builderCreate := &remote.BuilderCreate{
		Addr:   addr.String(),
		ApiKey: serverApiKey,
	}

	builderCreateBytes, err := json.Marshal(builderCreate)

	req, err := http.NewRequest("POST", fmt.Sprintf("http://%s/builder/create", *connectAddr), bytes.NewReader(builderCreateBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", string(apiKey))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	for {
		time.Sleep(1 * time.Hour)
	}
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
