package netstack

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

const (
	UDP_BUFFER_SIZE   = 8192
	HOST_CHANNEL_MTU  = 8192
	HOST_CHANNEL_SIZE = 256
)

func generateMacAddress() (net.HardwareAddr, error) {
	// Generate a MAC Address
	// Based on https://stackoverflow.com/questions/21018729/generate-mac-address-in-go
	buf := make([]byte, 3)

	_, err := rand.Read(buf)
	if err != nil {
		return nil, err
	}

	// Make the address local.
	buf[0] |= 2

	// Always start MacAddress's with the prefix of 88:75:56
	// This gives a small chase of collision (1 in 16 million) though.
	return net.HardwareAddr(
		append([]byte{0x88, 0x75, 0x56}, buf...),
	), nil
}

type NetworkInterface struct {
	NetSend    string
	NetRecv    string
	MacAddress string

	udpConn *net.UDPConn

	channel *channel.Endpoint
}

func (nic *NetworkInterface) onReceivePacket(pkt []byte) {
	// dstMac := net.HardwareAddr(pkt[:6])
	// srcMac := net.HardwareAddr(pkt[6:12])
	etherType := binary.BigEndian.Uint16(pkt[12:14])
	payload := pkt[14:]

	var proto tcpip.NetworkProtocolNumber

	if etherType == uint16(arp.ProtocolNumber) {
		proto = arp.ProtocolNumber
	} else if etherType == uint16(ipv4.ProtocolNumber) {
		proto = ipv4.ProtocolNumber
	} else if etherType == uint16(ipv6.ProtocolNumber) {
		proto = ipv6.ProtocolNumber
	} else {
		slog.Warn("nets: unknown protocol number", "proto", proto)
	}

	// slog.Info("pkt", "dst", dstMac.String(), "src", srcMac.String(), "etherType", etherType, "payload", payload)

	pktBuf := stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(payload),
	})

	nic.channel.InjectInbound(proto, pktBuf)
}

type NetStack struct {
	nStack     *stack.Stack
	interfaces []*NetworkInterface
	nextNicId  int
	packetDump *pcapgo.Writer
}

func (ns *NetStack) splitAddress(addr string) (tcpip.FullAddress, error) {
	var (
		ip  netip.Addr
		err error
	)
	tokens := strings.Split(addr, ":")

	if tokens[0] != "" {
		if tokens[0] == "0.0.0.0" {
			ip = netip.MustParseAddr("255.255.255.255")
		} else {
			ip, err = netip.ParseAddr(tokens[0])
			if err != nil {
				return tcpip.FullAddress{}, err
			}
		}
	} else {
		ip = netip.AddrFrom4([4]byte{10, 42, 0, 1})
	}

	port, err := strconv.Atoi(tokens[1])
	if err != nil {
		return tcpip.FullAddress{}, err
	}

	return tcpip.FullAddress{
		Addr: tcpip.AddrFromSlice(ip.AsSlice()),
		Port: uint16(port),
	}, nil
}

func (ns *NetStack) ListenInternal(network string, address string) (net.Listener, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("ListenInternal not implemented for network: %v", network)
	}

	addr, err := ns.splitAddress(address)
	if err != nil {
		return nil, err
	}

	return gonet.ListenTCP(ns.nStack, addr, ipv4.ProtocolNumber)
}

func (ns *NetStack) AttachNetworkInterface() (*NetworkInterface, error) {
	nic := &NetworkInterface{}

	hostMac, err := generateMacAddress()
	if err != nil {
		return nil, err
	}

	ns.nextNicId += 1

	nicId := tcpip.NICID(ns.nextNicId)

	nic.channel = channel.New(HOST_CHANNEL_SIZE, HOST_CHANNEL_MTU, tcpip.LinkAddress(hostMac))

	tcpErr := ns.nStack.CreateNIC(nicId, nic.channel)
	if tcpErr != nil {
		return nil, fmt.Errorf("tcpip error: %v", tcpErr)
	}

	// Enable forwarding on the host-attached nic.
	_, tcpErr = ns.nStack.SetNICForwarding(nicId, ipv4.ProtocolNumber, true)
	if tcpErr != nil {
		return nil, fmt.Errorf("tcpip error: %v", tcpErr)
	}
	_, tcpErr = ns.nStack.SetNICForwarding(nicId, ipv6.ProtocolNumber, true)
	if tcpErr != nil {
		return nil, fmt.Errorf("tcpip error: %v", tcpErr)
	}

	tcpErr = ns.nStack.AddProtocolAddress(nicId, tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			PrefixLen: 16,
			Address:   tcpip.AddrFromSlice([]byte{10, 42, 0, 1}),
		},
	}, stack.AddressProperties{})
	if tcpErr != nil {
		return nil, fmt.Errorf("tcpip error: %v", tcpErr)
	}

	remoteAddr, addrErr := netip.ParseAddr("0.0.0.0")
	if addrErr != nil {
		return nil, err
	}

	subnet, addrErr := tcpip.NewSubnet(tcpip.AddrFromSlice(remoteAddr.AsSlice()), tcpip.MaskFromBytes([]byte{0x00, 0x00, 0x00, 0x00}))
	if addrErr != nil {
		return nil, err
	}

	ns.nStack.AddRoute(tcpip.Route{
		Destination: subnet,
		Gateway:     subnet.ID(),
		NIC:         1,
	})

	send, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		return nil, err
	}

	nic.NetSend = send.LocalAddr().String()

	go func() {
		buf := make([]byte, 1500)

		for {
			n, err := send.Read(buf)
			if err != nil {
				slog.Error("failed to read send socket", "err", err)
				return
			}

			pkt := buf[:n]

			// slog.Info("got packet", "data", pkt)

			if ns.packetDump != nil {
				ns.packetDump.WritePacket(gopacket.CaptureInfo{
					CaptureLength: len(pkt),
					Length:        len(pkt),
				}, pkt)
			}

			nic.onReceivePacket(buf[:n])
		}
	}()

	recv, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		return nil, err
	}

	nic.NetRecv = recv.LocalAddr().String()

	recvPort := recv.LocalAddr().(*net.UDPAddr).Port

	if err := recv.Close(); err != nil {
		return nil, err
	}

	nic.udpConn, err = net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: recvPort,
	})
	if err != nil {
		return nil, err
	}

	deviceMac, err := generateMacAddress()
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			pkt := nic.channel.ReadContext(context.Background())

			pktBytes := make([]byte, pkt.Size()+14)

			copy(pktBytes[:6], deviceMac)
			copy(pktBytes[6:12], hostMac)
			binary.BigEndian.PutUint16(pktBytes[12:14], uint16(pkt.NetworkProtocolNumber))

			off := 14

			for _, slice := range pkt.AsSlices() {
				off += copy(pktBytes[off:], slice)
			}

			// slog.Info("got packet from host", "pktBytes", pktBytes)

			if ns.packetDump != nil {
				ns.packetDump.WritePacket(gopacket.CaptureInfo{
					CaptureLength: len(pktBytes),
					Length:        len(pktBytes),
				}, pktBytes)
			}

			_, err := nic.udpConn.Write(pktBytes)
			if err != nil {
				slog.Error("failed to write packet to guest", "err", err)
				return
			}

			pkt.DecRef()
		}
	}()

	nic.MacAddress = deviceMac.String()

	ns.interfaces = append(ns.interfaces, nic)

	return nic, nil
}

func (ns *NetStack) OpenPacketCapture(w io.Writer) error {
	writer := pcapgo.NewWriter(w)

	err := writer.WriteFileHeader(65536, layers.LinkTypeEthernet)
	if err != nil {
		return err
	}

	ns.packetDump = writer

	return nil
}

func New() *NetStack {
	ns := NetStack{}

	ns.nStack = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol, arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol6, icmp.NewProtocol4},
		HandleLocal:        true,
	})

	return &ns
}
