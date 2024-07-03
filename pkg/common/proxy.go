package common

import (
	"io"
	"net"
)

// Based on: https://gist.github.com/jbardin/821d08cb64c01c84b81a

type connLikeObject interface {
	io.Reader
	io.Writer
	io.Closer
}

func Proxy(srvConn, cliConn connLikeObject, bufferSize int) error {
	// channels to wait on the close event for each connection
	serverClosed := make(chan struct{}, 1)
	clientClosed := make(chan struct{}, 1)
	errC := make(chan error, 1)

	go broker(srvConn, cliConn, bufferSize, clientClosed, errC)
	go broker(cliConn, srvConn, bufferSize, serverClosed, errC)

	// wait for one half of the proxy to exit, then trigger a shutdown of the
	// other half by calling CloseRead(). This will break the read loop in the
	// broker and allow us to fully close the connection cleanly without a
	// "use of closed network connection" error.
	var waitFor chan struct{}
	select {
	case <-clientClosed:
		// the client closed first and any more packets from the server aren't
		// useful, so we can optionally SetLinger(0) here to recycle the port
		// faster.
		if srvConn, ok := srvConn.(*net.TCPConn); ok {
			_ = srvConn.SetLinger(0)
			_ = srvConn.CloseRead()
		}
		waitFor = serverClosed
	case <-serverClosed:
		if cliConn, ok := srvConn.(*net.TCPConn); ok {
			_ = cliConn.CloseRead()
		}
		waitFor = clientClosed
	case err := <-errC: // We have received an error.
		// Close both ends immediately.
		if srvConn, ok := srvConn.(*net.TCPConn); ok {
			_ = srvConn.SetLinger(0)
			_ = srvConn.CloseRead()
		}
		if cliConn, ok := srvConn.(*net.TCPConn); ok {
			_ = cliConn.CloseRead()
		}

		return err
	}

	// Wait for the other connection to close.
	// This "waitFor" pattern isn't required, but gives us a way to track the
	// connection and ensure all copies terminate correctly; we can trigger
	// stats on entry and deferred exit of this function.
	<-waitFor

	return nil
}

// This does the actual data transfer.
// The broker only closes the Read side.
func broker(dst, src connLikeObject, bufferSize int, srcClosed chan struct{}, errC chan error) {
	buf := make([]byte, bufferSize)

	// We can handle errors in a finer-grained manner by inlining io.Copy (it's
	// simple, and we drop the ReaderFrom or WriterTo checks for
	// net.Conn->net.Conn transfers, which aren't needed). This would also let
	// us adjust buffersize.
	_, err := io.CopyBuffer(dst, src, buf)

	if err != nil {
		// Ensure that the source is closed.
		src.Close()
		errC <- err
	}
	if err := src.Close(); err != nil {
		errC <- err
	}
	srcClosed <- struct{}{}
}
