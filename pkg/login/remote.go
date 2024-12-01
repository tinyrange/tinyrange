package login

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/chzyer/readline"
	"github.com/tinyrange/tinyrange/pkg/remote"
	"golang.org/x/crypto/ssh"
)

// remotes are done in the form of SSH like URLs (e.g. user@host:tinyrange)
func parseRemote(s string) (user string, host string, executable string, err error) {
	return "", "", "", fmt.Errorf("not implemented")
}

func loadSSHKeys() ([]ssh.Signer, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	var ret []ssh.Signer

	for _, key := range []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"} {
		keyPath, err := filepath.Abs(filepath.Join(homeDir, ".ssh", key))
		if err != nil {
			return nil, err
		}

		if info, err := os.Stat(keyPath); err != nil || info.IsDir() {
			continue
		}

		keyContents, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, err
		}

		signer, err := ssh.ParsePrivateKey(keyContents)
		if err != nil {
			return nil, err
		}

		ret = append(ret, signer)
	}

	return ret, nil
}

type RemoteSession struct {
	client         *ssh.Client
	mainSession    *ssh.Session
	mainWriter     io.WriteCloser
	remoteHttp     *http.Client
	remoteHttpPort int
}

func (r *RemoteSession) Close() error {
	if err := r.mainWriter.Close(); err != nil {
		return err
	}

	if err := r.mainSession.Close(); err != nil {
		return err
	}

	return r.client.Close()
}

func (r *RemoteSession) handleConnectionPipe(reader io.Reader) {
	dec := json.NewDecoder(reader)

	var msg remote.SessionMessage

	for {
		if err := dec.Decode(&msg); err != nil {
			slog.Warn("error decoding message", "err", err)

			return
		}

		switch msg.Kind {
		default:
			slog.Warn("unknown message kind", "kind", msg.Kind)
		}
	}
}

func (r *RemoteSession) ensureHttp() error {
	if r.remoteHttp != nil {
		return nil
	}

	// Ensure that the HTTP API is running on the other session.
	return nil
}

func (r *RemoteSession) RunConfig(config Config) error {
	if err := r.ensureHttp(); err != nil {
		return err
	}

	// Trigger a remote build of the basic definition first.
	// This returns a config file.

	// Dial the API to create the VM and login to it.
	// This is a websocket session with the same format as webssh.

	return fmt.Errorf("not implemented")
}

func EstablishSession(remote string) (*RemoteSession, error) {
	user, host, executable, err := parseRemote(remote)
	if err != nil {
		return nil, err
	}

	signers, err := loadSSHKeys()
	if err != nil {
		return nil, err
	}

	// Dial a SSH connection to the remote host
	client, err := ssh.Dial("tcp", host, &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signers...),
			ssh.PasswordCallback(func() (string, error) {
				// Read password
				fmt.Fprintf(os.Stderr, "Password: ")

				pass, err := readline.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return "", err
				}

				return string(pass), nil
			}),
		},
	})
	if err != nil {
		return nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}

	sess := &RemoteSession{
		client:      client,
		mainSession: session,
	}

	reader, err := sess.mainSession.StdoutPipe()
	if err != nil {
		return nil, err
	}

	go sess.handleConnectionPipe(reader)

	writer, err := sess.mainSession.StdinPipe()
	if err != nil {
		return nil, err
	}

	sess.mainWriter = writer

	if err := session.Start(executable); err != nil {
		return nil, err
	}

	return sess, nil
}

func RunConfig(remote string, config Config) error {
	session, err := EstablishSession(remote)
	if err != nil {
		return err
	}
	defer session.Close()

	return session.RunConfig(config)
}
