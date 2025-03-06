package vmm

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tinyrange/tinyrange/pkg/netstack"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type SecureSSHConfig struct {
	HostKey   string `json:"ssh_host_key"`
	PublicKey string `json:"ssh_public_key"`
	Password  string `json:"ssh_password"`
}

var ErrInterrupt = errors.New("Interrupt")
var ErrRestart = errors.New("Restart")

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

type closeType byte

const (
	closeExit closeType = iota
	closeRestart
)

type stdinWrap struct {
	io.Reader
	close chan closeType
}

// Read implements io.Reader.
func (s *stdinWrap) Read(p []byte) (n int, err error) {
	// Read the underlying reader first.
	n, err = s.Reader.Read(p)
	if err != nil {
		return
	}

	// Look for the interrupt char (CTRL-B) and return an error if that's encountered.
	if n := bytes.IndexByte(p[:n], 0x02); n != -1 {
		slog.Info("activating emergency restart")
		s.close <- closeRestart
		return 0, ErrInterrupt
	}

	return
}

var (
	_ io.Reader = &stdinWrap{}
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
	ns *netstack.NetStack,
	address string,
	username string,
	secureSSH SecureSSHConfig,
	exited *exitNotify,
) error {
	start := time.Now()

	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(secureSSH.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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
					slog.Debug("failed to connect", "err", err)
				}
			}
			continue
		}

		c, chans, reqs, err = ssh.NewClientConn(conn, address, config)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				if !strings.Contains(err.Error(), "connection was refused") {
					slog.Debug("failed to connect", "err", err)
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

	slog.Debug("connected over SSH", "took", time.Since(start))

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
		slog.Debug("detected non-interactive session")

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

	close := make(chan closeType, 1)

	if nonInteractive {
		reader := &waitReader{closed: make(chan bool)}
		defer reader.Close()

		session.Stdin = reader
	} else {
		session.Stdin = &stdinWrap{Reader: os.Stdin, close: close}
	}
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if err := session.Shell(); err != nil {
		return fmt.Errorf("failed to start shell: %v", err)
	}

	go func() {
		if err := session.Wait(); err != nil {
			if errors.Is(err, &ssh.ExitMissingError{}) {
				slog.Debug("failed to wait", "error", err)
			} else {
				slog.Warn("failed to wait", "error", err)
			}
		}

		close <- closeExit
	}()

	switch <-close {
	case closeExit:
		return nil
	case closeRestart:
		return ErrRestart
	}

	return nil
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

func newWebSocketSSH(ws *websocket.Conn, ns *netstack.NetStack, address string, username string, secureSSH SecureSSHConfig) error {
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(secureSSH.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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
				slog.Debug("failed to connect", "err", err)
			}
			continue
		}

		c, chans, reqs, err = ssh.NewClientConn(conn, address, config)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				slog.Debug("failed to connect", "err", err)
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
				slog.Warn("failed to read stdout", "error", err)
				break
			}

			_, err = wsWriter.Write(buf[:n])
			if err != nil {
				slog.Warn("failed to write to socket", "error", err)
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
				slog.Warn("failed to resize wsssh window", "error", err)
			}
		} else {
			_, err = stdin.Write([]byte(inputEv.Input))
			if err != nil {
				return fmt.Errorf("failed to write to stdin: %v", err)
			}
		}
	}
}
