package vmm

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"

	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/netstack/ns"
)

type SecureSSHConfig struct {
	HostKey   string `json:"ssh_host_key"`
	PublicKey string `json:"ssh_public_key"`
	Password  string `json:"ssh_password"`
	// AuthorizedKey is the user's public key allowed to access the guest.
	AuthorizedKey string `json:"ssh_authorized_key"`
	// ClientPrivateKey is the private key used by the host client to connect.
	// Not written to the guest init args; only persisted locally when using --secure-ssh or --name.
	ClientPrivateKey string `json:"ssh_client_private_key"`
}

var ErrInterrupt = errors.New("Interrupt")

type waitReader struct {
	closed   chan bool
	isClosed bool
}

// Close implements io.ReadCloser.
func (w *waitReader) Close() error {
	if !w.isClosed {
		close(w.closed)
		w.isClosed = true
	}

	return nil
}

// Read implements io.Reader.
func (w *waitReader) Read(p []byte) (n int, err error) {
	<-w.closed

	return 0, io.EOF
}

var (
	_ io.ReadCloser = &waitReader{}
)

// FdReader is an io.Reader with an Fd function
type FdReader interface {
	io.Reader
	Fd() uintptr
}

func getFd(reader io.Reader) (fd int, ok bool) {
	fdthing, ok := reader.(FdReader)
	if !ok {
		return 0, false
	}

	fd = int(fdthing.Fd())
	return fd, term.IsTerminal(fd)
}

type exitNotify struct {
	val atomic.Bool
}

func (e *exitNotify) Set() {
	e.val.Store(true)
}

func (e *exitNotify) Get() bool {
	return e.val.Load()
}

func connectOverSsh(
	ns ns.NetStack,
	log log.Handler,
	address string,
	username string,
	secureSSH SecureSSHConfig,
	exited *exitNotify,
) error {
	start := time.Now()

	config := &ssh.ClientConfig{
		User:            username,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Prefer key-based auth if a client private key is available.
	var auth []ssh.AuthMethod
	if secureSSH.ClientPrivateKey != "" {
		if signer, err := ssh.ParsePrivateKey([]byte(secureSSH.ClientPrivateKey)); err == nil {
			auth = append(auth, ssh.PublicKeys(signer))
		} else {
			log.Warn("failed to parse client private key; falling back if password present", "err", err)
		}
	}
	// Try SSH agent if available (works with user-provided .pub keys and encrypted keys).
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			ag := agent.NewClient(conn)
			auth = append(auth, ssh.PublicKeysCallback(ag.Signers))
			// Do not close conn here; callback may need it during handshake.
		} else {
			log.Debug("ssh-agent not available", "err", err)
		}
	}
	if secureSSH.Password != "" {
		auth = append(auth, ssh.Password(secureSSH.Password))
	}
	// Avoid empty auth which would fail fast; leave it empty if truly none are provided.
	if len(auth) > 0 {
		config.Auth = auth
	}

	if secureSSH.PublicKey != "" {
		public, _, _, _, err := ssh.ParseAuthorizedKey([]byte(secureSSH.PublicKey))
		if err != nil {
			return fmt.Errorf("failed to parse public key: %v", err)
		}

		config.HostKeyCallback = ssh.FixedHostKey(public)
	}

	var (
		conn  net.Conn
		c     ssh.Conn
		chans <-chan ssh.NewChannel
		reqs  <-chan *ssh.Request
		err   error
	)

	for {
		if exited.Get() {
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		conn, err = ns.DialInternalContext(ctx, "tcp", address)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				if !strings.Contains(err.Error(), "connection was refused") {
					log.Debug("failed to connect", "err", err)
				}
			}
			continue
		}

		c, chans, reqs, err = ssh.NewClientConn(conn, address, config)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				if !strings.Contains(err.Error(), "connection was refused") {
					log.Debug("failed to connect", "err", err)
				}
			}
			continue
		}

		break
	}

	client := ssh.NewClient(c, chans, reqs)

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	log.Debug("connected over SSH", "took", time.Since(start))

	width, height := 80, 40

	nonInteractive := false

	if fd, ok := getFd(os.Stdin); ok {
		state, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("failed to make terminal raw: %v", err)
		}
		defer func() { _ = term.Restore(fd, state) }()

		if w, h, err := getAndWatchSize(fd, session); err == nil {
			width, height = w, h
		}
	} else {
		log.Debug("detected non-interactive session")

		nonInteractive = true
	}

	term, ok := os.LookupEnv("TERM")
	if !ok {
		term = "linux"
	}

	if nonInteractive {
		term = "non-interactive/" + term
	}

	if err := session.RequestPty(term, height, width, ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}); err != nil {
		return fmt.Errorf("failed to request pty: %v", err)
	}

	errorChan := make(chan error, 1)
	closeChan := make(chan struct{}, 1)

	if nonInteractive {
		reader := &waitReader{closed: make(chan bool)}
		defer reader.Close()

		session.Stdin = reader
	} else {
		session.Stdin = os.Stdin
	}
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if err := session.Shell(); err != nil {
		return fmt.Errorf("failed to start shell: %v", err)
	}

	go func() {
		if err := session.Wait(); err != nil {
			if _, ok := err.(*ssh.ExitMissingError); ok {
				// Ignore missing exit errors
				log.Debug("ignoring missing exit error", "err", err)
			} else {
				errorChan <- err
			}
		}

		closeChan <- struct{}{}
	}()

	select {
	case err := <-errorChan:
		return err
	case <-closeChan:
		return nil
	}
}

type webSocketWriter struct {
	underlyingStream *websocket.Conn
	recorder         io.WriteCloser
}

// Close implements io.WriteCloser.
func (w *webSocketWriter) Close() error {
	if w.recorder != nil {
		return w.recorder.Close()
	}

	return nil
}

// Write implements io.WriteCloser.
func (w *webSocketWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	// Always try to write to the user first.
	s := base64.StdEncoding.EncodeToString(p)

	err = w.underlyingStream.WriteJSON(&struct {
		Output string `json:"output"`
	}{s})
	if err != nil {
		return -1, err
	}

	// WebSockets are message oriented so short writes are not possible.
	return len(p), nil
}

var (
	_ io.WriteCloser = &webSocketWriter{}
)

func newWebSocketSSH(ws *websocket.Conn, ns ns.NetStack, log log.Handler, address string, username string, secureSSH SecureSSHConfig) error {
	config := &ssh.ClientConfig{
		User:            username,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	var auth []ssh.AuthMethod
	if secureSSH.ClientPrivateKey != "" {
		if signer, err := ssh.ParsePrivateKey([]byte(secureSSH.ClientPrivateKey)); err == nil {
			auth = append(auth, ssh.PublicKeys(signer))
		} else {
			log.Warn("failed to parse client private key; falling back if password present", "err", err)
		}
	}
	// Agent support for webssh, same as terminal client.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			ag := agent.NewClient(conn)
			auth = append(auth, ssh.PublicKeysCallback(ag.Signers))
		} else {
			log.Debug("ssh-agent not available", "err", err)
		}
	}
	if secureSSH.Password != "" {
		auth = append(auth, ssh.Password(secureSSH.Password))
	}
	if len(auth) > 0 {
		config.Auth = auth
	}

	if secureSSH.PublicKey != "" {
		public, _, _, _, err := ssh.ParseAuthorizedKey([]byte(secureSSH.PublicKey))
		if err != nil {
			return fmt.Errorf("failed to parse public key: %v", err)
		}

		config.HostKeyCallback = ssh.FixedHostKey(public)
	}

	var (
		conn  net.Conn
		c     ssh.Conn
		chans <-chan ssh.NewChannel
		reqs  <-chan *ssh.Request
		err   error
	)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		conn, err = ns.DialInternalContext(ctx, "tcp", address)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				log.Debug("failed to connect", "err", err)
			}
			continue
		}

		c, chans, reqs, err = ssh.NewClientConn(conn, address, config)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				log.Debug("failed to connect", "err", err)
			}
			continue
		}

		break
	}

	client := ssh.NewClient(c, chans, reqs)

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	if err := session.RequestPty("xterm-256color", 25, 80, ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}); err != nil {
		return fmt.Errorf("failed to request pty: %v", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to pipe stdin: %v", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to pipe stdout: %v", err)
	}
	defer stdin.Close()

	if err := session.Shell(); err != nil {
		return fmt.Errorf("failed to start shell: %v", err)
	}

	wsWriter := &webSocketWriter{underlyingStream: ws}
	defer wsWriter.Close()

	go func() {
		for {
			// Pipe output to the websocket
			buf := make([]byte, 1024)

			n, err := stdout.Read(buf)
			if err != nil {
				log.Warn("failed to read stdout", "error", err)
				break
			}

			_, err = wsWriter.Write(buf[:n])
			if err != nil {
				log.Warn("failed to write to socket", "error", err)
				break
			}
		}
	}()

	for {
		var inputEv struct {
			Resize bool   `json:"resize"`
			Rows   int    `json:"rows"`
			Cols   int    `json:"cols"`
			Input  string `json:"input"`
		}
		// Get input from the websocket
		err := ws.ReadJSON(&inputEv)
		if err != nil {
			return fmt.Errorf("failed to read json: %v", err)
		}

		if inputEv.Resize {
			err := session.WindowChange(inputEv.Rows, inputEv.Cols)
			if err != nil {
				log.Warn("failed to resize wsssh window", "error", err)
			}
		} else {
			_, err = stdin.Write([]byte(inputEv.Input))
			if err != nil {
				return fmt.Errorf("failed to write to stdin: %v", err)
			}
		}
	}
}
