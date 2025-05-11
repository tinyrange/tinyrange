package ns

import (
	"context"
	"net"
)

type NetStack interface {
	ListenPacketInternal(network, address string) (net.PacketConn, error)
	DialInternalContext(ctx context.Context, network, address string) (net.Conn, error)
	ListenInternal(network, address string) (net.Listener, error)
}
