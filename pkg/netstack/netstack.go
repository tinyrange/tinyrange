package netstack

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/log"
)

type Wireguard interface {
	Dial(network, address string) (net.Conn, error)
}

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
	ns  *NetStack
	log log.Handler

	netSend    net.Addr
	netRecv    net.Addr
	MacAddress net.HardwareAddr

	udpConn *net.UDPConn

	channel *channel.Endpoint
}

func (nic *NetworkInterface) GetUDPSocketPair() (net.Addr, net.Addr, error) {
	if nic.netSend != nil {
		return nic.netSend, nic.netRecv, nil
	}

	send, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		return nil, nil, err
	}

	nic.netSend = send.LocalAddr()

	go func() {
		buf := make([]byte, 8192)

		for {
			n, _, err := send.ReadFromUDP(buf)
			if err != nil {
				nic.log.Error("failed to read send socket", "err", err)
				return
			}

			pkt := buf[:n]

			// log.Default().Info("got packet from client", "data", pkt)

			if nic.ns.packetDump != nil {
				nic.ns.packetDump.WritePacket(gopacket.CaptureInfo{
					CaptureLength: len(pkt),
					Length:        len(pkt),
				}, pkt)
			}

			nic.onReceivePacket(buf[:n])
		}
	}()

	recv, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		return nil, nil, err
	}

	nic.netRecv = recv.LocalAddr()

	recvPort := recv.LocalAddr().(*net.UDPAddr).Port

	if err := recv.Close(); err != nil {
		return nil, nil, err
	}

	nic.udpConn, err = net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: recvPort,
	})
	if err != nil {
		return nil, nil, err
	}

	go func() {
		for {
			pkt := nic.channel.ReadContext(context.Background())

			pktBytes := make([]byte, pkt.Size()+14)

			copy(pktBytes[:6], nic.MacAddress)
			copy(pktBytes[6:12], nic.ns.hostMac)
			binary.BigEndian.PutUint16(pktBytes[12:14], uint16(pkt.NetworkProtocolNumber))

			off := 14

			for _, slice := range pkt.AsSlices() {
				off += copy(pktBytes[off:], slice)
			}

			// log.Default().Info("got packet from host", "pktBytes", pktBytes)

			if nic.ns.packetDump != nil {
				nic.ns.packetDump.WritePacket(gopacket.CaptureInfo{
					CaptureLength: len(pktBytes),
					Length:        len(pktBytes),
				}, pktBytes)
			}

			_, err := nic.udpConn.Write(pktBytes)
			if err != nil {
				nic.log.Debug("failed to write packet to guest", "err", err)
			}

			pkt.DecRef()
		}
	}()

	return nic.netSend, nic.netRecv, nil
}

func (nic *NetworkInterface) AttachFile(file *os.File) error {
	// handle packets from the guest
	go func() {
		buf := make([]byte, 8192)

		for {
			n, err := file.Read(buf)
			if err != nil {
				nic.log.Error("failed to read send socket", "err", err)
				return
			}

			pkt := buf[:n]

			// log.Default().Info("got packet from client", "data", pkt)

			if nic.ns.packetDump != nil {
				nic.ns.packetDump.WritePacket(gopacket.CaptureInfo{
					CaptureLength: len(pkt),
					Length:        len(pkt),
				}, pkt)
			}

			nic.onReceivePacket(buf[:n])
		}
	}()

	// handle packets from the host
	go func() {
		for {
			pkt := nic.channel.ReadContext(context.Background())

			pktBytes := make([]byte, pkt.Size()+14)

			copy(pktBytes[:6], nic.MacAddress)
			copy(pktBytes[6:12], nic.ns.hostMac)
			binary.BigEndian.PutUint16(pktBytes[12:14], uint16(pkt.NetworkProtocolNumber))

			off := 14

			for _, slice := range pkt.AsSlices() {
				off += copy(pktBytes[off:], slice)
			}

			// log.Default().Info("got packet from host", "pktBytes", pktBytes)

			if nic.ns.packetDump != nil {
				nic.ns.packetDump.WritePacket(gopacket.CaptureInfo{
					CaptureLength: len(pktBytes),
					Length:        len(pktBytes),
				}, pktBytes)
			}

			_, err := file.Write(pktBytes)
			if err != nil {
				nic.log.Debug("failed to write packet to guest", "err", err)
			}

			pkt.DecRef()
		}
	}()

	return nil
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
		nic.log.Warn("nets: unknown protocol number", "proto", proto)
	}

	// log.Default().Info("pkt", "dst", dstMac.String(), "src", srcMac.String(), "etherType", etherType, "payload", payload)

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
	wg         Wireguard
	hostMac    net.HardwareAddr
	log        log.Handler

	// Configurable addressing (defaults to 10.42.0.{1,2,100}).
	hostGatewayIPv4 [4]byte
	guestIPv4       [4]byte
	serviceIPv4     [4]byte
	allowInternet   bool

	guestMacOverride net.HardwareAddr
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
		ip = netip.AddrFrom4(ns.hostGatewayIPv4)
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

func (ns *NetStack) DialInternalContext(ctx context.Context, network string, address string) (net.Conn, error) {
	addr, err := ns.splitAddress(address)
	if err != nil {
		return nil, err
	}

	switch network {
	case "tcp", "tcp4", "tcp6":
		return gonet.DialContextTCP(ctx, ns.nStack, addr, ipv4.ProtocolNumber)
	case "udp":
		// log.Printf("Dial UDP %+v", addr)
		return gonet.DialUDP(ns.nStack, nil, &addr, ipv4.ProtocolNumber)
	default:
		return nil, fmt.Errorf("DialInternal not implemented for network: %v", network)
	}
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

func (ns *NetStack) ListenPacketInternal(network string, address string) (net.PacketConn, error) {
	if network != "udp" && network != "udp4" && network != "udp6" {
		return nil, fmt.Errorf("ListenPacketInternal not implemented for network: %v", network)
	}

	addr, err := ns.splitAddress(address)
	if err != nil {
		return nil, err
	}

	if addr.Addr.String() == "255.255.255.255" {
		var queue waiter.Queue

		ep, tcpErr := ns.nStack.NewEndpoint(udp.ProtocolNumber, ipv4.ProtocolNumber, &queue)
		if tcpErr != nil {
			return nil, fmt.Errorf("tcpip error: %v", tcpErr)
		}

		// HACK FOR DHCP
		// Enable broadcast option.
		ep.SocketOptions().SetBroadcast(true)

		tcpErr = ep.Bind(addr)
		if tcpErr != nil {
			return nil, fmt.Errorf("tcpip error: %v", tcpErr)
		}

		udpConn := gonet.NewUDPConn(&queue, ep)

		return udpConn, nil
	} else {
		conn, err := gonet.DialUDP(ns.nStack, &addr, nil, ipv4.ProtocolNumber)
		if err != nil {
			return nil, err
		}

		return conn, nil
	}
}

func (ns *NetStack) AttachNetworkInterface() (*NetworkInterface, error) {
	var err error

	nic := &NetworkInterface{
		ns:  ns,
		log: ns.log,
	}

	ns.hostMac, err = generateMacAddress()
	if err != nil {
		return nil, err
	}

	ns.nextNicId += 1

	nicId := tcpip.NICID(ns.nextNicId)

	nic.channel = channel.New(HOST_CHANNEL_SIZE, HOST_CHANNEL_MTU, tcpip.LinkAddress(ns.hostMac))

	if err := ns.nStack.CreateNIC(nicId, nic.channel); err != nil {
		return nil, fmt.Errorf("tcpip error: %v", err)
	}

	sackEnabledOpt := tcpip.TCPSACKEnabled(true) // TCP SACK is disabled by default
	if err := ns.nStack.SetTransportProtocolOption(
		tcp.ProtocolNumber,
		&sackEnabledOpt,
	); err != nil {
		return nil, fmt.Errorf("could not enable TCP SACK: %v", err)
	}

	// Enable forwarding on the host-attached nic.
	if _, err := ns.nStack.SetNICForwarding(nicId, ipv4.ProtocolNumber, true); err != nil {
		return nil, fmt.Errorf("tcpip error: %v", err)
	}
	if _, err := ns.nStack.SetNICForwarding(nicId, ipv6.ProtocolNumber, true); err != nil {
		return nil, fmt.Errorf("tcpip error: %v", err)
	}

	if err := ns.nStack.AddProtocolAddress(nicId, tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			PrefixLen: 16,
			Address:   tcpip.AddrFromSlice([]byte{10, 42, 0, 1}),
		},
	}, stack.AddressProperties{
		PEB:        stack.CanBePrimaryEndpoint, // zero value default
		ConfigType: stack.AddressConfigStatic,  // zero value default
	}); err != nil {
		return nil, fmt.Errorf("tcpip error: %v", err)
	}

	ns.nStack.AddRoute(tcpip.Route{Destination: header.IPv4EmptySubnet, NIC: 1})

	// Maybe needed due to https://github.com/google/gvisor/issues/3876
	// seems to break the networking with it enabled though.
	if err := ns.nStack.SetPromiscuousMode(nicId, true); err != nil {
		return nil, fmt.Errorf("failed to set promiscuous mode: %s", err)
	}

	// Enable spoofing on the nic so we can get addresses for the internet
	// sites the guest reaches out to.
	if err := ns.nStack.SetSpoofing(nicId, true); err != nil {
		return nil, fmt.Errorf("failed to set spoofing mode: %s", err)
	}

	if ns.guestMacOverride != nil {
		nic.MacAddress = ns.guestMacOverride
	} else {
		deviceMac, err := generateMacAddress()
		if err != nil {
			return nil, err
		}
		nic.MacAddress = deviceMac
	}

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

func (ns *NetStack) handleTcpForward(r *tcp.ForwarderRequest) {
	id := r.ID()

	var wq waiter.Queue

	ep, ipErr := r.CreateEndpoint(&wq)
	if ipErr != nil {
		ns.log.Error("error creating endpoint", "err", ipErr)
		r.Complete(true)
		return
	}

	if id.LocalAddress.As4() == ns.hostGatewayIPv4 {
		// Connections to the host should have already been handled.
		r.Complete(true)
		return
	}

	r.Complete(false)
	ep.SocketOptions().SetDelayOption(true)

	conn := gonet.NewTCPConn(&wq, ep)
	go func() {
		var err error

		defer conn.Close()

		loc := &net.TCPAddr{
			IP:   net.IP(id.LocalAddress.AsSlice()),
			Port: int(id.LocalPort),
		}

		ns.log.Debug("dialing remote host", "addr", loc.String())

		var outbound net.Conn

		if ns.wg != nil {
			outbound, err = ns.wg.Dial("tcp", loc.String())
		} else {
			// Proxy connections to 10.42.0.100 to localhost.
			if id.LocalAddress.As4() == ns.serviceIPv4 {
				loc.IP = net.IPv4(127, 0, 0, 1)
			}

			// Enforce internet access policy: if disabled, only allow host gateway and service IP.
			if !ns.allowInternet {
				dst := id.LocalAddress.As4()
				if dst != ns.hostGatewayIPv4 && dst != ns.serviceIPv4 {
					// Close without dialing out; connection will reset/close.
					_ = conn.Close()
					return
				}
			}

			outbound, err = net.DialTCP("tcp", nil, loc)
		}
		if err != nil {
			return
		}
		defer outbound.Close()

		if err := common.Proxy(outbound, conn, 1400); err != nil {
			return
		}
	}()
}

// func (ns *NetStack) handleUdpForward(r *udp.ForwarderRequest) {
// 	log.Default().Info("udp forwarding request", "req", r)
// }

func New(log log.Handler) *NetStack {
	ns := NetStack{
		log: log,
	}

	// Default addressing compatible with previous behavior.
	ns.hostGatewayIPv4 = [4]byte{10, 42, 0, 1}
	ns.guestIPv4 = [4]byte{10, 42, 0, 2}
	ns.serviceIPv4 = [4]byte{10, 42, 0, 100}
	ns.allowInternet = true

	ns.nStack = stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{
			ipv4.NewProtocol,
			ipv6.NewProtocol,
			arp.NewProtocol,
		},
		TransportProtocols: []stack.TransportProtocolFactory{
			tcp.NewProtocol,
			udp.NewProtocol,
			icmp.NewProtocol6,
			icmp.NewProtocol4,
		},
		// HandleLocal: true,
	})

	const tcpReceiveBufferSize = 0
	const maxInFlightConnectionAttempts = 1024
	tcpFwd := tcp.NewForwarder(ns.nStack, tcpReceiveBufferSize, maxInFlightConnectionAttempts, ns.handleTcpForward)
	ns.nStack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpFwd.HandlePacket)

	// fwdUdp := udp.NewForwarder(ns.nStack, ns.handleUdpForward)
	// ns.nStack.SetTransportProtocolHandler(udp.ProtocolNumber, fwdUdp.HandlePacket)

	return &ns
}

// SetNetworkParams sets the host/guest/service IPv4 addresses and internet policy.
// Inputs must be dotted-quad IPv4 strings.
func (ns *NetStack) SetNetworkParams(hostGateway, guest, service string, allowInternet bool) error {
	parse4 := func(s string) ([4]byte, error) {
		ip := net.ParseIP(s)
		if ip == nil {
			return [4]byte{}, fmt.Errorf("invalid ipv4: %s", s)
		}
		ip4 := ip.To4()
		if ip4 == nil {
			return [4]byte{}, fmt.Errorf("not an ipv4 address: %s", s)
		}
		return [4]byte{ip4[0], ip4[1], ip4[2], ip4[3]}, nil
	}

	var err error
	if hostGateway != "" {
		ns.hostGatewayIPv4, err = parse4(hostGateway)
		if err != nil {
			return err
		}
	}
	if guest != "" {
		ns.guestIPv4, err = parse4(guest)
		if err != nil {
			return err
		}
	}
	if service != "" {
		ns.serviceIPv4, err = parse4(service)
		if err != nil {
			return err
		}
	}
	ns.allowInternet = allowInternet
	return nil
}

// SetGuestMAC sets a specific MAC address for the guest NIC.
func (ns *NetStack) SetGuestMAC(mac string) error {
	if mac == "" {
		ns.guestMacOverride = nil
		return nil
	}
	m, err := net.ParseMAC(mac)
	if err != nil {
		return err
	}
	if len(m) != 6 {
		return fmt.Errorf("invalid MAC address length: %d", len(m))
	}
	ns.guestMacOverride = m
	return nil
}
