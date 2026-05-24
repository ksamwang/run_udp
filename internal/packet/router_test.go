package packet

import (
	"encoding/binary"
	"errors"
	"net"
	"testing"

	"udp_tunnel_demo/internal/store"
)

func TestRouteOutboundTCPToVirtualAddress(t *testing.T) {
	router := newTestRouter(t, RouterConfig{
		NetworkID: 1, SourceDeviceID: "dev-a", MTU: 1280,
		Addresses: []store.VirtualAddress{
			{NetworkID: 1, DeviceID: "dev-a", VirtualIP: "172.16.10.1"},
			{NetworkID: 1, DeviceID: "dev-b", VirtualIP: "172.16.10.2"},
		},
		PeerAvailable: map[string]bool{"dev-b": true},
	})

	frame, err := router.RouteOutbound(tcpSYNPacket(t, "172.16.10.1", "172.16.10.2", 50000, 3389, 1460))
	if err != nil {
		t.Fatal(err)
	}
	if frame.NetworkID != 1 || frame.SrcDevice != "dev-a" || frame.DstDevice != "dev-b" || frame.PacketType != TypeIPv4 {
		t.Fatalf("bad frame metadata: %+v", frame)
	}
	if frame.Header.Protocol != IPv4ProtocolTCP || frame.Header.DestPort != 3389 {
		t.Fatalf("bad parsed header: %+v", frame.Header)
	}
	stats := router.Stats()
	if stats.TxPackets != 1 || stats.TxBytes == 0 || stats.Drops != (DropStats{}) {
		t.Fatalf("bad stats: %+v", stats)
	}
}

func TestRouteOutboundUDPToVirtualAddress(t *testing.T) {
	router := newTestRouter(t, RouterConfig{
		NetworkID: 1, SourceDeviceID: "dev-a", MTU: 1280,
		Addresses: []store.VirtualAddress{
			{NetworkID: 1, DeviceID: "dev-a", VirtualIP: "172.16.10.1"},
			{NetworkID: 1, DeviceID: "dev-b", VirtualIP: "172.16.10.2"},
		},
		PeerAvailable: map[string]bool{"dev-b": true},
	})

	frame, err := router.RouteOutbound(udpPacket(t, "172.16.10.1", "172.16.10.2", 50000, 53, []byte{1, 2, 3}))
	if err != nil {
		t.Fatal(err)
	}
	if frame.DstDevice != "dev-b" || frame.Header.Protocol != IPv4ProtocolUDP || frame.Header.DestPort != 53 {
		t.Fatalf("bad udp route: %+v", frame)
	}
}

func TestRouteOutboundUDPDenyACL(t *testing.T) {
	router := newTestRouter(t, RouterConfig{
		NetworkID: 1, SourceDeviceID: "dev-a", MTU: 1280,
		Addresses: []store.VirtualAddress{{NetworkID: 1, DeviceID: "dev-b", VirtualIP: "172.16.10.2"}},
		ACLRules: []store.VirtualACLRule{{
			NetworkID: 1, SourceDeviceID: "dev-a", TargetDeviceID: "dev-b",
			Protocol: "udp", PortStart: 53, PortEnd: 53, Action: "deny", Enabled: true,
		}},
		PeerAvailable: map[string]bool{"dev-b": true},
	})

	_, err := router.RouteOutbound(udpPacket(t, "172.16.10.1", "172.16.10.2", 50000, 53, []byte{1}))
	if !errors.Is(err, ErrACLDeny) {
		t.Fatalf("expected udp acl deny, got %v", err)
	}
}

func TestRouteOutboundDenyACL(t *testing.T) {
	router := newTestRouter(t, RouterConfig{
		NetworkID: 1, SourceDeviceID: "dev-a", MTU: 1280,
		Addresses: []store.VirtualAddress{{NetworkID: 1, DeviceID: "dev-b", VirtualIP: "172.16.10.2"}},
		ACLRules: []store.VirtualACLRule{{
			NetworkID: 1, SourceDeviceID: "dev-a", TargetDeviceID: "dev-b",
			Protocol: "tcp", PortStart: 3389, PortEnd: 3389, Action: "deny", Enabled: true,
		}},
	})

	_, err := router.RouteOutbound(tcpSYNPacket(t, "172.16.10.1", "172.16.10.2", 50000, 3389, 1200))
	if !errors.Is(err, ErrACLDeny) {
		t.Fatalf("expected acl deny, got %v", err)
	}
	if got := router.Stats().Drops.ACLDeny; got != 1 {
		t.Fatalf("acl deny drops=%d", got)
	}
}

func TestRouteOutboundDropReasons(t *testing.T) {
	tests := []struct {
		name string
		cfg  RouterConfig
		pkt  []byte
		err  error
		want func(DropStats) uint64
	}{
		{
			name: "route miss",
			cfg:  RouterConfig{NetworkID: 1, SourceDeviceID: "dev-a", MTU: 1280},
			pkt:  tcpSYNPacket(t, "172.16.10.1", "172.16.10.99", 50000, 3389, 1200),
			err:  ErrRouteMiss,
			want: func(d DropStats) uint64 { return d.RouteMiss },
		},
		{
			name: "mtu",
			cfg:  RouterConfig{NetworkID: 1, SourceDeviceID: "dev-a", MTU: 40},
			pkt:  tcpSYNPacket(t, "172.16.10.1", "172.16.10.2", 50000, 3389, 1200),
			err:  ErrMTUDrop,
			want: func(d DropStats) uint64 { return d.MTU },
		},
		{
			name: "peer unavailable",
			cfg: RouterConfig{
				NetworkID: 1, SourceDeviceID: "dev-a", MTU: 1280,
				Addresses:     []store.VirtualAddress{{NetworkID: 1, DeviceID: "dev-b", VirtualIP: "172.16.10.2"}},
				PeerAvailable: map[string]bool{"dev-b": false},
			},
			pkt:  tcpSYNPacket(t, "172.16.10.1", "172.16.10.2", 50000, 3389, 1200),
			err:  ErrPeerUnavailable,
			want: func(d DropStats) uint64 { return d.PeerUnavailable },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter(t, tt.cfg)
			_, err := router.RouteOutbound(tt.pkt)
			if !errors.Is(err, tt.err) {
				t.Fatalf("expected %v, got %v", tt.err, err)
			}
			if got := tt.want(router.Stats().Drops); got != 1 {
				t.Fatalf("drop count=%d stats=%+v", got, router.Stats())
			}
		})
	}
}

func TestRouterUpdateConfigAddsPeerRoute(t *testing.T) {
	router := newTestRouter(t, RouterConfig{
		NetworkID: 1, SourceDeviceID: "dev-a", MTU: 1280,
		Addresses: []store.VirtualAddress{{NetworkID: 1, DeviceID: "dev-a", VirtualIP: "172.16.10.1"}},
	})

	_, err := router.RouteOutbound(tcpSYNPacket(t, "172.16.10.1", "172.16.10.2", 50000, 3389, 1200))
	if !errors.Is(err, ErrRouteMiss) {
		t.Fatalf("expected initial route miss, got %v", err)
	}

	router.UpdateConfig([]store.VirtualAddress{
		{NetworkID: 1, DeviceID: "dev-a", VirtualIP: "172.16.10.1"},
		{NetworkID: 1, DeviceID: "dev-b", VirtualIP: "172.16.10.2"},
	}, nil, map[string]bool{"dev-b": true})

	frame, err := router.RouteOutbound(tcpSYNPacket(t, "172.16.10.1", "172.16.10.2", 50000, 3389, 1200))
	if err != nil {
		t.Fatal(err)
	}
	if frame.DstDevice != "dev-b" {
		t.Fatalf("dst device=%s", frame.DstDevice)
	}
}

func TestParseIPv4RejectsBadPacket(t *testing.T) {
	if _, err := ParseIPv4([]byte{0x45}); !errors.Is(err, ErrInvalidIPv4) {
		t.Fatalf("expected invalid ipv4, got %v", err)
	}
	pkt := tcpSYNPacket(t, "172.16.10.1", "172.16.10.2", 1, 2, 1200)
	pkt[0] = 0x65
	if _, err := ParseIPv4(pkt); !errors.Is(err, ErrInvalidIPv4) {
		t.Fatalf("expected invalid version, got %v", err)
	}
}

func TestClampTCPMSS(t *testing.T) {
	pkt := tcpSYNPacket(t, "172.16.10.1", "172.16.10.2", 50000, 3389, 1460)
	if !ClampTCPMSS(pkt, 1200) {
		t.Fatal("expected mss clamp")
	}
	header, err := ParseIPv4(pkt)
	if err != nil {
		t.Fatal(err)
	}
	tcp := pkt[header.HeaderLen:header.TotalLen]
	got := binary.BigEndian.Uint16(tcp[22:24])
	if got != 1200 {
		t.Fatalf("mss=%d", got)
	}
	if checksum(pkt[:header.HeaderLen]) != 0 {
		t.Fatal("bad ipv4 header checksum")
	}
	tcpChecksum := binary.BigEndian.Uint16(tcp[16:18])
	tcp[16], tcp[17] = 0, 0
	recomputeTCPChecksum(pkt[:header.TotalLen], header)
	if binary.BigEndian.Uint16(tcp[16:18]) != tcpChecksum {
		t.Fatal("bad tcp checksum")
	}
}

func newTestRouter(t *testing.T, cfg RouterConfig) *Router {
	t.Helper()
	router, err := NewRouter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func tcpSYNPacket(t *testing.T, src, dst string, srcPort, dstPort, mss int) []byte {
	t.Helper()
	srcIP := net.ParseIP(src).To4()
	dstIP := net.ParseIP(dst).To4()
	if srcIP == nil || dstIP == nil {
		t.Fatal("bad test ip")
	}
	ipHeaderLen := 20
	tcpHeaderLen := 24
	totalLen := ipHeaderLen + tcpHeaderLen
	pkt := make([]byte, totalLen)
	pkt[0] = 0x45
	pkt[1] = 0
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 1)
	binary.BigEndian.PutUint16(pkt[6:8], 0x4000)
	pkt[8] = 64
	pkt[9] = IPv4ProtocolTCP
	copy(pkt[12:16], srcIP)
	copy(pkt[16:20], dstIP)
	recomputeIPv4HeaderChecksum(pkt[:ipHeaderLen])

	tcp := pkt[ipHeaderLen:]
	binary.BigEndian.PutUint16(tcp[0:2], uint16(srcPort))
	binary.BigEndian.PutUint16(tcp[2:4], uint16(dstPort))
	binary.BigEndian.PutUint32(tcp[4:8], 1)
	tcp[12] = byte(tcpHeaderLen/4) << 4
	tcp[13] = 0x02
	binary.BigEndian.PutUint16(tcp[14:16], 65535)
	tcp[20] = 2
	tcp[21] = 4
	binary.BigEndian.PutUint16(tcp[22:24], uint16(mss))
	header, err := ParseIPv4(pkt)
	if err != nil {
		t.Fatal(err)
	}
	recomputeTCPChecksum(pkt, header)
	return pkt
}

func udpPacket(t *testing.T, src, dst string, srcPort, dstPort int, payload []byte) []byte {
	t.Helper()
	srcIP := net.ParseIP(src).To4()
	dstIP := net.ParseIP(dst).To4()
	if srcIP == nil || dstIP == nil {
		t.Fatal("bad test ip")
	}
	ipHeaderLen := 20
	udpHeaderLen := 8
	totalLen := ipHeaderLen + udpHeaderLen + len(payload)
	pkt := make([]byte, totalLen)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 1)
	binary.BigEndian.PutUint16(pkt[6:8], 0x4000)
	pkt[8] = 64
	pkt[9] = IPv4ProtocolUDP
	copy(pkt[12:16], srcIP)
	copy(pkt[16:20], dstIP)
	recomputeIPv4HeaderChecksum(pkt[:ipHeaderLen])

	udp := pkt[ipHeaderLen:]
	binary.BigEndian.PutUint16(udp[0:2], uint16(srcPort))
	binary.BigEndian.PutUint16(udp[2:4], uint16(dstPort))
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpHeaderLen+len(payload)))
	copy(udp[udpHeaderLen:], payload)
	return pkt
}
