package tinyrange

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/tinyrange/tinyrange/pkg/netstack"
	"github.com/tinyrange/tinyrange/pkg/vnc/client"
)

func runVncClient(ns *netstack.NetStack, address string) error {
	var (
		conn net.Conn
		err  error
	)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		conn, err = ns.DialInternalContext(ctx, "tcp", address)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				slog.Debug("failed to connect", "err", err)
			}

			time.Sleep(50 * time.Millisecond)

			continue
		}

		break
	}

	err = client.RunVNCClient(conn)
	if err != nil {
		slog.Error("VNC client crashed", "err", err)
		return err
	}

	return nil
}
