package main

import (
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
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		conn, err = ns.DialInternalContext(ctx, "tcp", address)
		if err != nil {
			slog.Warn("failed to connect", "err", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		c, chans, reqs, err = ssh.NewClientConn(conn, address, config)
		if err != nil {
			slog.Warn("failed to connect", "err", err)
			time.Sleep(100 * time.Millisecond)
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

	fd, ok := getFd(os.Stdin)
	if ok {
		state, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("failed to make terminal raw: %v", err)
		}
		defer func() { _ = term.Restore(fd, state) }()
	}

	term := "linux"

	if err := session.RequestPty(term, height, width, ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}); err != nil {
		return fmt.Errorf("failed to request pty: %v", err)
	}

	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if err := session.Shell(); err != nil {
		time.Sleep(100 * time.Millisecond)
		return fmt.Errorf("failed to start shell: %v", err)
	}

	if err := session.Wait(); err != nil {
		if errors.Is(err, &ssh.ExitMissingError{}) {
			slog.Debug("failed to wait", "error", err)
		} else {
			slog.Warn("failed to wait", "error", err)
		}
	}

	return nil
}
