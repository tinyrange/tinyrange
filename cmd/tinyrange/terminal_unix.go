//go:build !windows
// +build !windows

// From: https://github.com/superfly/flyctl/blob/master/ssh/terminal_unix.go (Apache-2.0)

package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func getAndWatchSize(fd int, sess *ssh.Session) (int, int, error) {
	width, height, err := term.GetSize(fd)
	if err != nil {
		return 0, 0, err
	}

	go func() {
		if err := watchWindowSize(fd, sess); err != nil {
			slog.Warn("Error watching window size", "error", err)
		}
	}()

	return width, height, nil
}

func watchWindowSize(fd int, sess *ssh.Session) error {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGWINCH)

	for {
		<-sigc

		width, height, err := term.GetSize(fd)
		if err != nil {
			return err
		}

		if err := sess.WindowChange(height, width); err != nil {
			return err
		}
	}
}
