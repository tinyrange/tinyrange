//go:build windows
// +build windows

// From: https://github.com/superfly/flyctl/blob/master/ssh/terminal_windows.go (Apache-2.0)

package tinyrange

import (
	"log/slog"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/windows"
)

func getAndWatchSize(_ int, sess *ssh.Session) (int, int, error) {
	// TODO(): Hardcoded stdout instead of pulling it from the SessionIO because it's
	//            wrapped in multiple wrapper types.
	fd := windows.Stdout

	width, height, err := getConsoleSize(fd)
	if err != nil {
		return 0, 0, err
	}

	go func() {
		if err := watchWindowSize(fd, sess, width, height); err != nil {
			slog.Debug("Error watching window size", "error", err)
		}
	}()

	return width, height, nil
}

func watchWindowSize(fd windows.Handle, sess *ssh.Session, width int, height int) error {
	// NOTE(): Windows doesn't support SIGWINCH. The closest it has is WINDOW_BUFFER_SIZE_EVENT,
	// which you only seem to be able to receive if *all* of your console input is read with ReadConsoleInput.
	// (I'm also unsure how portable this is, it *might* just be a Windows Terminal thing, I didn't research too hard)
	// That's a huge undertaking, even *if* you stubbed stdin with a pipe and had a goroutine hydrating it from
	// the ReadConsoleInput data. (getting these types into go is a nightmare given the C unions, and I'm not quite
	// sure how to force everything in flyctl down the road to know that the pipe stdin is in fact a terminal)
	//
	// Because of this, we resort to the oldest trick in the book: polling! Sorry.

	for {
		select {
		case <-time.After(200 * time.Millisecond):
		}

		newWidth, newHeight, err := getConsoleSize(fd)
		if err != nil {
			return err
		}

		if newWidth == width && newHeight == height {
			continue
		}

		width = newWidth
		height = newHeight

		if err := sess.WindowChange(height, width); err != nil {
			return err
		}
	}
}

func getConsoleSize(fd windows.Handle) (int, int, error) {
	var csbi windows.ConsoleScreenBufferInfo
	err := windows.GetConsoleScreenBufferInfo(fd, &csbi)
	if err != nil {
		return 0, 0, err
	}

	// Cannot use csbi.Size here because it represents a size of
	// the buffer (which includes scrollback) but not the size of
	// the window.
	width := csbi.Window.Right - csbi.Window.Left + 1
	height := csbi.Window.Bottom - csbi.Window.Top + 1

	return int(width), int(height), nil
}
