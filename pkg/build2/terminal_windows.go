//go:build windows

package build2

func getAndWatchSize(fd int,
	closeChan chan struct{},
	errChan chan error,
	resizeChan chan termSize,
) {
}
