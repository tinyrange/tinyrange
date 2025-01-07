//go:build unix

package build2

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

func getAndWatchSize(fd int,
	closeChan chan struct{},
	errChan chan error,
	resizeChan chan termSize,
) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGWINCH)
	defer signal.Stop(sigc)

	for {
		select {
		case <-closeChan:
			return
		case <-sigc:
			width, height, err := term.GetSize(fd)
			if err != nil {
				errChan <- err
				return
			}

			resizeChan <- termSize{width: width, height: height}
		}
	}
}
