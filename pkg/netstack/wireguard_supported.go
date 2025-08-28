//go:build linux || darwin || freebsd || openbsd || windows

package netstack

import (
	"context"
	"fmt"

	"github.com/tinyrange/wireguard"

	"github.com/tinyrange/tinyrange/pkg/common"
)

func (ns *NetStack) SetupWireguard(config string, mtu int) error {
	handler := wireguard.NewSimpleFlowHandler()

	wg, err := wireguard.NewFromConfig("10.40.0.2", mtu, config, handler)
	if err != nil {
		return err
	}

	ns.wg = wg

	// Use the connection so it establishes with the server.
	go func() {
		conn, _ := ns.wg.Dial("tcp", "10.40.0.1:8080")
		if conn != nil {
			conn.Close()
		}
	}()

	listen, err := handler.ListenTCPAddr(fmt.Sprintf("%d.%d.%d.%d:0", ns.guestIPv4[0], ns.guestIPv4[1], ns.guestIPv4[2], ns.guestIPv4[3]))
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := listen.Accept()
			if err != nil {
				ns.log.Error("failed to accept connection", "err", err)
				return
			}

			go func() {
				defer conn.Close()

				backend, err := ns.DialInternalContext(context.Background(), "tcp", conn.LocalAddr().String())
				if err != nil {
					ns.log.Error("failed to dial backend", "err", err)
					return
				}
				defer backend.Close()

				if err := common.Proxy(backend, conn, 1400); err != nil {
					ns.log.Error("proxy error", "err", err)
				}
			}()
		}
	}()

	return nil
}
