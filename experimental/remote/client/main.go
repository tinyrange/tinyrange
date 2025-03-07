package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"

	"github.com/google/uuid"
	"github.com/tinyrange/tinyrange/experimental/remote"
	"github.com/tinyrange/tinyrange/pkg/path"
	"golang.org/x/crypto/ssh"
)

var (
	server        = flag.String("server", "localhost:22", "server to connect to")
	user          = flag.String("user", "joshua", "user to connect as")
	serverCommand = flag.String("server-cmd", "cd ~/dev/projects/tinyrange2;go run github.com/tinyrange/tinyrange/experimental/remote/server", "command to run on the server")
)

// use the system ssh configuration
func getSSHConfig() (*ssh.ClientConfig, error) {
	// attempt to discover private keys in the default locations

	config := &ssh.ClientConfig{}
	config.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	sshDir := path.Native.Join(homeDir, ".ssh")

	if _, err := os.Stat(sshDir); err != nil {
		// no keys found
		return config, nil
	}

	var signers []ssh.Signer

	for _, name := range []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"} {
		key := path.Native.Join(sshDir, name)
		_, err := os.Stat(key)
		if err == nil {
			key, err := os.ReadFile(key)
			if err != nil {
				return nil, err
			}

			signer, err := ssh.ParsePrivateKey(key)
			if err != nil {
				return nil, err
			}

			signers = append(signers, signer)
		}
	}

	if len(signers) > 0 {
		config.Auth = append(config.Auth, ssh.PublicKeys(signers...))
	}

	return config, nil
}

func appMain() error {
	// connect over ssh to *server as *user and run *serverCommand

	config, err := getSSHConfig()
	if err != nil {
		return fmt.Errorf("failed to get ssh config: %w", err)
	}
	config.User = *user

	conn, err := ssh.Dial("tcp", *server, config)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}

	slog.Info("connected")

	listen, err := conn.ListenTCP(&net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 0,
	})
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer listen.Close()

	mux := http.NewServeMux()

	apiKey := uuid.NewString()

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

	mux.HandleFunc("/builder/create", func(w http.ResponseWriter, r *http.Request) {
		var req remote.BuilderCreate

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "failed to decode", http.StatusBadRequest)
			return
		}

		slog.Info("builder create", "addr", req.Addr, "apiKey", req.ApiKey)
	})

	go http.Serve(listen, &remote.ApiKeyHandler{
		Handler: mux,
		ApiKey:  apiKey,
	})

	addr := listen.Addr().(*net.TCPAddr)

	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	session.Stdin = bytes.NewReader([]byte(apiKey))

	if err := session.Start(*serverCommand + fmt.Sprintf(" -connect %s:%d", addr.IP, addr.Port)); err != nil {
		return fmt.Errorf("failed to start: %w", err)
	}

	return session.Wait()
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
