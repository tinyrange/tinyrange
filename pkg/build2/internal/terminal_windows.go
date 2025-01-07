//go:build windows

package internal

func getAndWatchSize(fd int,
	closeChan chan struct{},
	errChan chan error,
	resizeChan chan termSize,
) {
}
