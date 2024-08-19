package tinyrange

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/tinyrange/tinyrange/pkg/netstack"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

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

func connectOverSsh(ns *netstack.NetStack, address string, username string, password string) error {
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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

	width, height := 80, 40

	nonInteractive := false

	fd, ok := getFd(os.Stdin)
	if ok {
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
