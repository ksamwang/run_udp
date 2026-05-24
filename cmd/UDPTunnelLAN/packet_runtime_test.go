package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"udp_tunnel_demo/internal/lan"
	"udp_tunnel_demo/internal/lantransport"
	"udp_tunnel_demo/internal/packet"
	"udp_tunnel_demo/internal/protocol"
	"udp_tunnel_demo/internal/secure"
	"udp_tunnel_demo/internal/store"
	"udp_tunnel_demo/internal/tunnel"
	"udp_tunnel_demo/internal/upnp"
)

func TestPostAndPollRelayFrames(t *testing.T) {
	var sent struct {
		DeviceID string          `json:"device_id"`
		Frames   []lanRelayFrame `json:"frames"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/lan/packets/send":
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]int{"accepted": len(sent.Frames)})
		case "/api/lan/packets/poll":
			_ = json.NewEncoder(w).Encode(map[string]any{"frames": []lanRelayFrame{{
				NetworkID: 7, SrcDevice: "dev-b", DstDevice: "dev-a", Type: packet.TypeIPv4, Payload: "AQID",
			}}})
		default:
			t.Fatalf("bad path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	err := postRelayFrames(context.Background(), srv.URL, []packet.RoutedFrame{{
		NetworkID: 7, SrcDevice: "dev-a", DstDevice: "dev-b", PacketType: packet.TypeIPv4, Payload: []byte{1, 2, 3},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if sent.DeviceID != "dev-a" || len(sent.Frames) != 1 || sent.Frames[0].Payload != "AQID" {
		t.Fatalf("bad sent request: %+v", sent)
	}

	frames, err := pollRelayFrames(context.Background(), srv.URL, "dev-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].SrcDevice != "dev-b" || frames[0].Payload != "AQID" {
		t.Fatalf("bad poll frames: %+v", frames)
	}
}

func TestRelayDisabledErrorIsVisible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "relay_disabled", "error": "LAN packet relay is disabled"})
	}))
	defer srv.Close()

	err := postRelayFrames(context.Background(), srv.URL, []packet.RoutedFrame{{
		NetworkID: 7, SrcDevice: "dev-a", DstDevice: "dev-b", PacketType: packet.TypeIPv4, Payload: []byte{1},
	}})
	if err == nil || !strings.Contains(err.Error(), "relay_disabled") {
		t.Fatalf("expected relay_disabled error, got %v", err)
	}
}

func TestSealRelayFramesRequiresPeerCodec(t *testing.T) {
	p := &lanP2P{peers: map[string]*lanP2PPeer{"dev-b": {id: "dev-b"}}}
	frames, err := p.SealRelayFrames([]packet.RoutedFrame{{SrcDevice: "dev-a", DstDevice: "dev-b", Payload: []byte{1, 2, 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatalf("relay must not send plaintext without codec: %+v", frames)
	}
}

func TestSealAndOpenRelayFrameUsesPacketCodec(t *testing.T) {
	privA, pubA, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	privB, pubB, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	txA, rxA, err := lanPeerCodecsFromX25519(privA, pubB, "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	txB, rxB, err := lanPeerCodecsFromX25519(privB, pubA, "dev-b", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	pA := &lanP2P{deviceID: "dev-a", peers: map[string]*lanP2PPeer{"dev-b": {id: "dev-b", tx: txA, rx: rxA}}}
	pB := &lanP2P{deviceID: "dev-b", peers: map[string]*lanP2PPeer{"dev-a": {id: "dev-a", tx: txB, rx: rxB}}}
	plain := []byte{0x45, 0, 1, 2}
	sealed, err := pA.SealRelayFrames([]packet.RoutedFrame{{NetworkID: 7, SrcDevice: "dev-a", DstDevice: "dev-b", PacketType: packet.TypeIPv4, Payload: plain}})
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) != 1 || string(sealed[0].Payload) == string(plain) {
		t.Fatalf("relay payload must be encrypted: %+v", sealed)
	}
	opened, err := pB.OpenRelayFrame(sealed[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(opened.Payload) != string(plain) {
		t.Fatalf("bad opened payload: %v", opened.Payload)
	}
}

func TestPostRelayFramesReceivesEncryptedPayload(t *testing.T) {
	encrypted := []byte("encrypted-frame")
	var got lanRelayFrame
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/lan/packets/send" {
			t.Fatalf("bad path: %s", r.URL.Path)
		}
		var req struct {
			DeviceID string          `json:"device_id"`
			Frames   []lanRelayFrame `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		got = req.Frames[0]
		_ = json.NewEncoder(w).Encode(map[string]int{"accepted": 1})
	}))
	defer srv.Close()

	if err := postRelayFrames(context.Background(), srv.URL, []packet.RoutedFrame{{
		NetworkID: 7, SrcDevice: "dev-a", DstDevice: "dev-b", PacketType: packet.TypeIPv4, Payload: encrypted,
	}}); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(encrypted) {
		t.Fatalf("postRelayFrames should transport already encrypted payload, got %q", decoded)
	}
}

func TestLANPendingQueuePrioritizesInteractiveFrames(t *testing.T) {
	q := newLANPendingQueue(8, 0, time.Minute)
	q.Add([]packet.RoutedFrame{
		{
			DstDevice: "dev-b", Payload: bytes.Repeat([]byte{1}, 1400),
			Header: packet.IPv4Header{Protocol: packet.IPv4ProtocolTCP, DestPort: 445, PayloadSize: 1360},
		},
		{
			DstDevice: "dev-b", Payload: []byte("syn"),
			Header: packet.IPv4Header{Protocol: packet.IPv4ProtocolTCP, DestPort: 3389, TCPSYN: true},
		},
		{
			DstDevice: "dev-b", Payload: []byte("icmp"),
			Header: packet.IPv4Header{Protocol: packet.IPv4ProtocolICMP},
		},
	})

	frames := q.Frames()
	if got := string(frames[0].Payload); got != "syn" {
		t.Fatalf("tcp syn should be first, got %q", got)
	}
	if got := string(frames[1].Payload); got != "icmp" {
		t.Fatalf("icmp should be second with stable priority order, got %q", got)
	}
	q.Remove(map[int]bool{0: true})
	frames = q.Frames()
	if len(frames) != 2 {
		t.Fatalf("expected two remaining frames, got %d", len(frames))
	}
	if got := string(frames[0].Payload); got != "icmp" {
		t.Fatalf("remove must follow prioritized order, got first remaining %q", got)
	}
	if len(frames[1].Payload) != 1400 {
		t.Fatalf("bulk frame should remain last, got %d bytes", len(frames[1].Payload))
	}
}

func TestLANPendingQueueKeepsCriticalFramesWhenFull(t *testing.T) {
	q := newLANPendingQueue(2, 0, time.Minute)
	q.Add([]packet.RoutedFrame{
		{
			DstDevice: "dev-b", Payload: bytes.Repeat([]byte{1}, 1400),
			Header: packet.IPv4Header{Protocol: packet.IPv4ProtocolTCP, DestPort: 445},
		},
		{
			DstDevice: "dev-b", Payload: []byte("syn"),
			Header: packet.IPv4Header{Protocol: packet.IPv4ProtocolTCP, DestPort: 3389, TCPSYN: true},
		},
		{
			DstDevice: "dev-b", Payload: []byte("dns"),
			Header: packet.IPv4Header{Protocol: packet.IPv4ProtocolUDP, DestPort: 53},
		},
	})
	frames := q.Frames()
	if len(frames) != 2 {
		t.Fatalf("expected two frames, got %d", len(frames))
	}
	if string(frames[0].Payload) != "syn" || string(frames[1].Payload) != "dns" {
		t.Fatalf("critical frames should be preserved, got %q/%q", frames[0].Payload, frames[1].Payload)
	}
	stats := q.Stats()
	if stats.Dropped != 1 || stats.Frames != 2 || stats.Added != 3 || stats.TCP != 2 || stats.UDP != 1 || stats.Interactive != 1 || stats.Throughput != 1 {
		t.Fatalf("bad pending stats: %+v", stats)
	}
}

func TestClassifyLANFrame(t *testing.T) {
	tests := []struct {
		name  string
		frame packet.RoutedFrame
		want  string
	}{
		{
			name:  "tcp syn critical",
			frame: packet.RoutedFrame{Header: packet.IPv4Header{Protocol: packet.IPv4ProtocolTCP, TCPSYN: true}, Payload: []byte("syn")},
			want:  lanTrafficCritical,
		},
		{
			name:  "rdp interactive",
			frame: packet.RoutedFrame{Header: packet.IPv4Header{Protocol: packet.IPv4ProtocolTCP, DestPort: 3389}, Payload: []byte("rdp")},
			want:  lanTrafficInteractive,
		},
		{
			name:  "smb throughput",
			frame: packet.RoutedFrame{Header: packet.IPv4Header{Protocol: packet.IPv4ProtocolTCP, DestPort: 445}, Payload: bytes.Repeat([]byte{1}, 1400)},
			want:  lanTrafficThroughput,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyLANFrame(tt.frame); got != tt.want {
				t.Fatalf("classifyLANFrame=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestLANWintunReadStatsClassifiesPackets(t *testing.T) {
	stats := &lanWintunReadStats{lastLog: time.Now()}
	icmp := make([]byte, 40)
	icmp[0] = 0x45
	icmp[3] = 40
	icmp[9] = packet.IPv4ProtocolICMP
	copy(icmp[12:16], []byte{172, 16, 10, 1})
	copy(icmp[16:20], []byte{172, 16, 10, 2})
	stats.recordPacket(icmp)
	stats.recordPacket(bytes.Repeat([]byte{0x45}, 1300))
	if stats.packets != 2 || stats.bytes != 1340 || stats.icmp != 1 || stats.large != 1 {
		t.Fatalf("bad wintun stats: %+v", stats)
	}
}

func BenchmarkRelayFrameEnvelopeEncoding(b *testing.B) {
	frame := lanRelayFrame{
		NetworkID: 7, SrcDevice: "dev-a", DstDevice: "dev-b", Type: packet.TypeIPv4,
		Payload: base64.StdEncoding.EncodeToString(make([]byte, 1024)),
	}
	b.Run("json-base64", func(b *testing.B) {
		payload := struct {
			DeviceID string          `json:"device_id"`
			Frames   []lanRelayFrame `json:"frames"`
		}{DeviceID: "dev-a", Frames: []lanRelayFrame{frame}}
		b.ReportAllocs()
		b.SetBytes(1024)
		for i := 0; i < b.N; i++ {
			buf, err := json.Marshal(payload)
			if err != nil {
				b.Fatal(err)
			}
			var out struct {
				DeviceID string          `json:"device_id"`
				Frames   []lanRelayFrame `json:"frames"`
			}
			if err := json.Unmarshal(buf, &out); err != nil {
				b.Fatal(err)
			}
			if len(out.Frames) != 1 {
				b.Fatal("missing frame")
			}
		}
	})
	b.Run("binary-envelope", func(b *testing.B) {
		payload := make([]byte, 1024)
		b.ReportAllocs()
		b.SetBytes(int64(len(payload)))
		for i := 0; i < b.N; i++ {
			buf := encodeBenchRelayBinaryFrame(7, "dev-a", "dev-b", packet.TypeIPv4, payload)
			networkID, src, dst, typ, decoded, err := decodeBenchRelayBinaryFrame(buf)
			if err != nil {
				b.Fatal(err)
			}
			if networkID != 7 || src != "dev-a" || dst != "dev-b" || typ != packet.TypeIPv4 || len(decoded) != len(payload) {
				b.Fatal("bad decoded frame")
			}
		}
	})
}

func encodeBenchRelayBinaryFrame(networkID int64, src, dst string, typ byte, payload []byte) []byte {
	buf := make([]byte, 0, 8+1+2+len(src)+2+len(dst)+4+len(payload))
	buf = binary.BigEndian.AppendUint64(buf, uint64(networkID))
	buf = append(buf, typ)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(src)))
	buf = append(buf, src...)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(dst)))
	buf = append(buf, dst...)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(payload)))
	buf = append(buf, payload...)
	return buf
}

func decodeBenchRelayBinaryFrame(buf []byte) (int64, string, string, byte, []byte, error) {
	r := bytes.NewReader(buf)
	var networkID uint64
	if err := binary.Read(r, binary.BigEndian, &networkID); err != nil {
		return 0, "", "", 0, nil, err
	}
	typ, err := r.ReadByte()
	if err != nil {
		return 0, "", "", 0, nil, err
	}
	src, err := readBenchBinaryString(r)
	if err != nil {
		return 0, "", "", 0, nil, err
	}
	dst, err := readBenchBinaryString(r)
	if err != nil {
		return 0, "", "", 0, nil, err
	}
	var n uint32
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return 0, "", "", 0, nil, err
	}
	payload := make([]byte, int(n))
	if _, err := r.Read(payload); err != nil {
		return 0, "", "", 0, nil, err
	}
	return int64(networkID), src, dst, typ, payload, nil
}

func readBenchBinaryString(r *bytes.Reader) (string, error) {
	var n uint16
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return "", err
	}
	buf := make([]byte, int(n))
	if _, err := r.Read(buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func TestSendPacketsUsesHTTPRelayWhenP2PUnavailable(t *testing.T) {
	privA, pubA, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	privB, pubB, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	txA, rxA, err := lanPeerCodecsFromX25519(privA, pubB, "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	_, rxB, err := lanPeerCodecsFromX25519(privB, pubA, "dev-b", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	var got lanRelayFrame
	posted := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/lan/packets/send" {
			t.Fatalf("bad path: %s", r.URL.Path)
		}
		var req struct {
			DeviceID string          `json:"device_id"`
			Frames   []lanRelayFrame `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.DeviceID != "dev-a" || len(req.Frames) != 1 {
			t.Fatalf("bad relay request: %+v", req)
		}
		got = req.Frames[0]
		posted <- struct{}{}
		_ = json.NewEncoder(w).Encode(map[string]int{"accepted": 1})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outbound := make(chan packet.RoutedFrame, 1)
	p2p := &lanP2P{deviceID: "dev-a", peers: map[string]*lanP2PPeer{"dev-b": {id: "dev-b", tx: txA, rx: rxA}}}
	link := packet.NewLinkManager(packet.LinkConfig{DeviceID: "dev-a"})
	go sendPackets(ctx, srv.URL, link, p2p, lanPathPolicyConfig{Name: "prefer_p2p"}, outbound)
	outbound <- packet.RoutedFrame{NetworkID: 7, SrcDevice: "dev-a", DstDevice: "dev-b", PacketType: packet.TypeIPv4, Payload: []byte("plain")}

	select {
	case <-posted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for HTTP relay fallback")
	}
	wire, err := base64.StdEncoding.DecodeString(got.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) == "plain" {
		t.Fatal("HTTP relay must not carry plaintext payload")
	}
	plain, err := rxB.Open(wire)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "plain" {
		t.Fatalf("bad relayed payload: %q", plain)
	}
}

func TestSendPacketsReplaysPendingWhenDatagramBecomesReady(t *testing.T) {
	left, right, err := udpPair()
	if err != nil {
		t.Fatal(err)
	}
	defer left.Close()
	defer right.Close()
	privA, pubA, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	privB, pubB, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	txA, rxA, err := lanPeerCodecsFromX25519(privA, pubB, "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	_, rxB, err := lanPeerCodecsFromX25519(privB, pubA, "dev-b", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	peer := &lanP2PPeer{id: "dev-b", tx: txA, rx: rxA}
	peer.addr.Store(right.LocalAddr().(*net.UDPAddr))
	p2p := &lanP2P{conn: left, deviceID: "dev-a", peers: map[string]*lanP2PPeer{"dev-b": peer}}
	link := packet.NewLinkManager(packet.LinkConfig{DeviceID: "dev-a"})
	_, _ = link.UpsertPeer(packet.PeerEndpoint{DeviceID: "dev-b", Addr: right.LocalAddr().String()}, packet.PeerEndpoint{Addr: "http-relay"}, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outbound := make(chan packet.RoutedFrame, 1)
	go sendPackets(ctx, "", link, p2p, lanPathPolicyConfig{Name: "prefer_p2p"}, outbound)
	outbound <- packet.RoutedFrame{NetworkID: 7, SrcDevice: "dev-a", DstDevice: "dev-b", PacketType: packet.TypeIPv4, Payload: []byte("queued")}
	time.Sleep(100 * time.Millisecond)
	peer.datagramReady.Store(true)

	buf := make([]byte, 2048)
	_ = right.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := right.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := rxB.Open(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "queued" {
		t.Fatalf("pending frame not replayed via datagram: %q", plain)
	}
}

func TestSendPacketsRelayOnlySkipsDatagramAndUsesUDPRelay(t *testing.T) {
	clientConn, serverConn, err := udpPair()
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	defer serverConn.Close()
	_, p2pConn, err := udpPair()
	if err != nil {
		t.Fatal(err)
	}
	defer p2pConn.Close()
	privA, pubA, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	privB, pubB, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	txA, rxA, err := lanPeerCodecsFromX25519(privA, pubB, "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	_, rxB, err := lanPeerCodecsFromX25519(privB, pubA, "dev-b", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	peer := &lanP2PPeer{id: "dev-b", tx: txA, rx: rxA}
	peer.addr.Store(p2pConn.LocalAddr().(*net.UDPAddr))
	peer.datagramReady.Store(true)
	p2p := &lanP2P{conn: clientConn, server: serverConn.LocalAddr().(*net.UDPAddr), deviceID: "dev-a", peers: map[string]*lanP2PPeer{"dev-b": peer}}
	link := packet.NewLinkManager(packet.LinkConfig{DeviceID: "dev-a"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outbound := make(chan packet.RoutedFrame, 1)
	go sendPackets(ctx, "", link, p2p, lanPathPolicyConfig{Name: "relay_only", PreferRelay: true, RelayOnly: true}, outbound)
	outbound <- packet.RoutedFrame{NetworkID: 7, SrcDevice: "dev-a", DstDevice: "dev-b", PacketType: packet.TypeIPv4, Payload: []byte("relay-only")}

	buf := make([]byte, 2048)
	_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := serverConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	relayFrame, err := lantransport.UnpackRelayFrame(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	plain, err := rxB.Open(relayFrame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "relay-only" {
		t.Fatalf("bad relay payload: %q", plain)
	}
	_ = p2pConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if n, _, err := p2pConn.ReadFromUDP(buf); err == nil {
		t.Fatalf("relay_only should not send direct datagram bytes=%d", n)
	}
}

func TestLANP2PSendPrefersDatagramBeforeKCP(t *testing.T) {
	udpLeft, udpRight, err := udpPair()
	if err != nil {
		t.Fatal(err)
	}
	defer udpLeft.Close()
	defer udpRight.Close()
	kcpLeft, kcpRight := net.Pipe()
	defer kcpLeft.Close()
	defer kcpRight.Close()
	privA, pubA, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	privB, pubB, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	txA, rxA, err := lanPeerCodecsFromX25519(privA, pubB, "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	_, rxB, err := lanPeerCodecsFromX25519(privB, pubA, "dev-b", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	peer := &lanP2PPeer{id: "dev-b", tx: txA, rx: rxA, kcp: kcpLeft}
	peer.addr.Store(udpRight.LocalAddr().(*net.UDPAddr))
	peer.datagramReady.Store(true)
	peer.connected.Store(true)
	p := &lanP2P{conn: udpLeft, deviceID: "dev-a", peers: map[string]*lanP2PPeer{"dev-b": peer}}

	if err := p.Send(packet.RoutedFrame{DstDevice: "dev-b", Payload: []byte("datagram")}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2048)
	_ = udpRight.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := udpRight.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := rxB.Open(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "datagram" {
		t.Fatalf("bad datagram payload: %q", plain)
	}
	_ = kcpRight.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if payload, err := readLANFrame(kcpRight); err == nil {
		t.Fatalf("KCP should not receive datagram-preferred payload: %q", payload)
	}
	if got := p.currentTrafficClass("dev-b"); got != lanTrafficInteractive {
		t.Fatalf("traffic class=%q, want %q", got, lanTrafficInteractive)
	}
	dataPath, reason := p.peerDataPath("dev-b")
	if dataPath != lanPathP2PDatagram || reason != "datagram_ready" {
		t.Fatalf("bad peer data path: path=%q reason=%q", dataPath, reason)
	}
}

func TestLANP2PSendFallsBackToKCPWhenDatagramUnavailable(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	peer := &lanP2PPeer{id: "dev-b", kcp: left}
	peer.connected.Store(true)
	p := &lanP2P{deviceID: "dev-a", peers: map[string]*lanP2PPeer{"dev-b": peer}}

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Send(packet.RoutedFrame{DstDevice: "dev-b", Payload: []byte("kcp")})
	}()
	got, err := readLANFrame(right)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if string(got) != "kcp" {
		t.Fatalf("bad fallback payload: %q", got)
	}
}

func TestLANP2PSendUDPRelayFrame(t *testing.T) {
	client, server, err := udpPair()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	defer server.Close()
	p := &lanP2P{conn: client, server: server.LocalAddr().(*net.UDPAddr), deviceID: "dev-a"}
	if err := p.SendUDPRelayFrame(packet.RoutedFrame{SrcDevice: "dev-a", DstDevice: "dev-b", Payload: []byte("ciphertext")}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	_ = server.SetReadDeadline(testDeadline())
	n, _, err := server.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := lantransport.UnpackRelayFrame(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if frame.SrcDevice != "dev-a" || frame.DstDevice != "dev-b" || string(frame.Payload) != "ciphertext" {
		t.Fatalf("bad relay frame: %+v", frame)
	}
}

func TestLANP2PPunchAckUsesDatagramWithoutOpeningKCP(t *testing.T) {
	privA, pubA, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	privB, pubB, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	txA, rxA, err := lanPeerCodecsFromX25519(privA, pubB, "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := lanPeerCodecsFromX25519(privB, pubA, "dev-b", "dev-a"); err != nil {
		t.Fatal(err)
	}
	peerAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 21000}
	peer := &lanP2PPeer{id: "dev-b", tx: txA, rx: rxA}
	p := &lanP2P{deviceID: "dev-a", peers: map[string]*lanP2PPeer{"dev-b": peer}}
	link := packet.NewLinkManager(packet.LinkConfig{DeviceID: "dev-a"})
	msg := &protocol.Message{Type: protocol.MsgPunchAck, From: "dev-b", Profile: store.ProfileLANPacket, Payload: lanDatagramReadyFrame}
	b, _ := protocol.Encode(msg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.handleControl(ctx, b, peerAddr, nil, nil, link)

	if !peer.punched.Load() || !peer.datagramReady.Load() {
		t.Fatalf("expected datagram-ready punch state, punched=%v datagram=%v", peer.punched.Load(), peer.datagramReady.Load())
	}
	if peer.connected.Load() || peer.kcp != nil || peer.pc != nil {
		t.Fatalf("datagram-ready direct path must not open KCP: connected=%v kcp=%v pc=%v", peer.connected.Load(), peer.kcp, peer.pc)
	}
	frame, err := link.Send("dev-b", []byte("probe"))
	if err != nil {
		t.Fatal(err)
	}
	if frame.Path != packet.LinkPathP2P {
		t.Fatalf("expected p2p link path, got %q", frame.Path)
	}
}

func TestLANP2PSendAfterPunch(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	peer := &lanP2PPeer{id: "dev-b", kcp: left}
	peer.connected.Store(true)
	p := &lanP2P{deviceID: "dev-a", peers: map[string]*lanP2PPeer{"dev-b": peer}}

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Send(packet.RoutedFrame{DstDevice: "dev-b", Payload: []byte{1, 2, 3}})
	}()
	got, err := readLANFrame(right)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if string(got) != string([]byte{1, 2, 3}) {
		t.Fatalf("bad packet data=%v", got)
	}
}

func TestLANP2PUpsertPeersRegistersNewPeer(t *testing.T) {
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	identity, err := lan.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	_, pub, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	p := &lanP2P{
		conn: client, server: server.LocalAddr().(*net.UDPAddr), deviceID: "dev-a",
		peers: map[string]*lanP2PPeer{}, registering: map[string]bool{}, x25519Pub: pub, upnpAddr: "203.0.113.9:40000",
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.UpsertPeers(ctx, identity, []lanBootstrapPeer{{DeviceID: "dev-b"}})

	buf := make([]byte, 1024)
	_ = server.SetReadDeadline(testDeadline())
	n, _, err := server.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf[:n]), `"t":"lan_register"`) || !strings.Contains(string(buf[:n]), `"p":"dev-b"`) {
		t.Fatalf("bad register payload: %s", string(buf[:n]))
	}
	if !strings.Contains(string(buf[:n]), `"u":"203.0.113.9:40000"`) {
		t.Fatalf("register must include upnp addr: %s", string(buf[:n]))
	}
}

func TestLANP2PStartsRepeatedPunchAfterPeerInfo(t *testing.T) {
	peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer peerConn.Close()
	clientConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	privA, _, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	_, pubB, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &lanP2P{
		conn: clientConn, deviceID: "dev-a", x25519Priv: privA,
		peers: map[string]*lanP2PPeer{"dev-b": {id: "dev-b"}}, registering: map[string]bool{},
	}
	link := packet.NewLinkManager(packet.LinkConfig{DeviceID: "dev-a"})
	msg := &protocol.Message{Type: protocol.MsgPeerInfo, Peer: "dev-b", Profile: "lan-packet", Addr: peerConn.LocalAddr().String(), Payload: pubB}
	b, _ := protocol.Encode(msg)
	p.handleControl(ctx, b, peerConn.LocalAddr().(*net.UDPAddr), nil, nil, link)

	buf := make([]byte, 1024)
	_ = peerConn.SetReadDeadline(testDeadline())
	n, _, err := peerConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf[:n]), `"t":"punch"`) {
		t.Fatalf("expected punch, got %s", string(buf[:n]))
	}
}

func TestLANP2PStartsPunchToUPnPAddrAfterPeerInfo(t *testing.T) {
	observedConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer observedConn.Close()
	upnpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer upnpConn.Close()
	clientConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	privA, _, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	_, pubB, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &lanP2P{
		conn: clientConn, deviceID: "dev-a", x25519Priv: privA,
		peers: map[string]*lanP2PPeer{"dev-b": {id: "dev-b"}}, registering: map[string]bool{},
	}
	link := packet.NewLinkManager(packet.LinkConfig{DeviceID: "dev-a"})
	msg := &protocol.Message{
		Type: protocol.MsgPeerInfo, Peer: "dev-b", Profile: "lan-packet",
		Addr: observedConn.LocalAddr().String(), UpnpAddr: upnpConn.LocalAddr().String(), Payload: pubB,
	}
	b, _ := protocol.Encode(msg)
	p.handleControl(ctx, b, observedConn.LocalAddr().(*net.UDPAddr), nil, nil, link)

	expectPunch := func(conn *net.UDPConn, label string) {
		t.Helper()
		buf := make([]byte, 1024)
		_ = conn.SetReadDeadline(testDeadline())
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("%s did not receive punch: %v", label, err)
		}
		if !strings.Contains(string(buf[:n]), `"t":"punch"`) {
			t.Fatalf("%s expected punch, got %s", label, string(buf[:n]))
		}
	}
	expectPunch(observedConn, "observed")
	expectPunch(upnpConn, "upnp")
}

func TestLANP2PPeerInfoKeepsReadyRelayMode(t *testing.T) {
	relayAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7000}
	directAddr := "203.0.113.10:40000"
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	p := &lanP2P{
		deviceID: "dev-a",
		peers: map[string]*lanP2PPeer{
			"dev-b": {id: "dev-b", kcp: left},
		},
	}
	peer := p.peers["dev-b"]
	peer.addr.Store(relayAddr)
	peer.isRelay.Store(true)
	peer.connected.Store(true)
	peer.punched.Store(true)
	peer.punching.Store(true)
	msg := &protocol.Message{Type: protocol.MsgPeerInfo, Peer: "dev-b", Profile: "lan-packet", Addr: directAddr}
	b, _ := protocol.Encode(msg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.handleControl(ctx, b, relayAddr, nil, nil, packet.NewLinkManager(packet.LinkConfig{DeviceID: "dev-a"}))
	cancel()
	if !peer.isRelay.Load() || !peer.connected.Load() || !peer.punched.Load() || !peer.punching.Load() {
		t.Fatalf("peer must keep ready relay mode: relay=%v connected=%v punched=%v punching=%v", peer.isRelay.Load(), peer.connected.Load(), peer.punched.Load(), peer.punching.Load())
	}
	if got := peer.addr.Load(); got == nil || got.String() != directAddr {
		t.Fatalf("peer info must install direct address, got %v", got)
	}
	if peer.kcp != left {
		t.Fatal("ready relay KCP must not be closed by peer info refresh")
	}
}

func TestLANP2PPeerInfoKeepsUnreadyRelayMode(t *testing.T) {
	relayAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7000}
	directAddr := "203.0.113.10:40000"
	p := &lanP2P{
		deviceID: "dev-a",
		peers: map[string]*lanP2PPeer{
			"dev-b": {id: "dev-b"},
		},
	}
	peer := p.peers["dev-b"]
	peer.addr.Store(relayAddr)
	peer.isRelay.Store(true)
	peer.punched.Store(true)
	msg := &protocol.Message{Type: protocol.MsgPeerInfo, Peer: "dev-b", Profile: "lan-packet", Addr: directAddr}
	b, _ := protocol.Encode(msg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.handleControl(ctx, b, relayAddr, nil, nil, packet.NewLinkManager(packet.LinkConfig{DeviceID: "dev-a"}))
	cancel()
	if !peer.isRelay.Load() || !peer.punched.Load() || !peer.punching.Load() {
		t.Fatalf("peer must keep unready relay mode: relay=%v punched=%v punching=%v", peer.isRelay.Load(), peer.punched.Load(), peer.punching.Load())
	}
	if got := peer.addr.Load(); got == nil || got.String() != directAddr {
		t.Fatalf("peer info must install direct address, got %v", got)
	}
}

func TestLANP2PPeerIDByPacketUsesKCPConvBeforeAddress(t *testing.T) {
	server := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7000}
	p := &lanP2P{
		deviceID: "dev-a",
		peers: map[string]*lanP2PPeer{
			"dev-b": {id: "dev-b"},
			"dev-c": {id: "dev-c"},
		},
		peerConv: map[uint32]string{},
	}
	p.peers["dev-b"].addr.Store(server)
	p.peers["dev-c"].addr.Store(server)
	convB := secure.ConvID("", "dev-a", "dev-b", store.ProfileLANPacket)
	convC := secure.ConvID("", "dev-a", "dev-c", store.ProfileLANPacket)
	p.peerConv[convB] = "dev-b"
	p.peerConv[convC] = "dev-c"

	var pkt [4]byte
	binary.LittleEndian.PutUint32(pkt[:], convC)
	if got := p.peerIDByPacket(pkt[:], server); got != "dev-c" {
		t.Fatalf("packet must be demuxed by conv id, got %q", got)
	}
}

func TestLANP2POpenFailureKeepsRelayAndRebuildsPacketConn(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	server := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7000}
	p := &lanP2P{
		conn: conn, server: server, deviceID: "dev-a",
		peers: map[string]*lanP2PPeer{}, openRetries: map[string]bool{},
	}
	peer := &lanP2PPeer{id: "dev-b"}
	peer.addr.Store(server)
	peer.pc = tunnel.NewPacketConn(conn, &peer.addr)
	oldPC := peer.pc
	peer.connected.Store(true)
	peer.punched.Store(true)
	peer.punching.Store(true)
	peer.isRelay.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.resetPeerAfterOpenFailure(ctx, peer, nil)

	if peer.connected.Load() {
		t.Fatal("connected must be cleared after open failure")
	}
	if !peer.punched.Load() || peer.punching.Load() || !peer.isRelay.Load() {
		t.Fatalf("bad relay state after open failure: punched=%v punching=%v relay=%v", peer.punched.Load(), peer.punching.Load(), peer.isRelay.Load())
	}
	if peer.pc == nil || peer.pc == oldPC {
		t.Fatal("packet conn must be rebuilt after open failure")
	}
	if got := peer.addr.Load(); got == nil || got.String() != server.String() {
		t.Fatalf("relay address not preserved: %v", got)
	}
	if !p.openRetries[peer.id] {
		t.Fatal("relay open failure must schedule a retry")
	}
}

func TestLANP2POpenFailureResetsDirectPunch(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	server := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7000}
	p := &lanP2P{
		conn: conn, server: server, deviceID: "dev-a",
		peers: map[string]*lanP2PPeer{}, relayTimers: map[string]bool{},
	}
	peer := &lanP2PPeer{id: "dev-b"}
	peer.connected.Store(true)
	peer.punched.Store(true)
	peer.punching.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.resetPeerAfterOpenFailure(ctx, peer, nil)

	if peer.connected.Load() || peer.punched.Load() || peer.punching.Load() || peer.isRelay.Load() {
		t.Fatalf("direct failure must reset punch state: connected=%v punched=%v punching=%v relay=%v", peer.connected.Load(), peer.punched.Load(), peer.punching.Load(), peer.isRelay.Load())
	}
	if !p.relayTimers[peer.id] {
		t.Fatal("direct open failure must restart relay fallback timer")
	}
}

func TestLANP2PSocketRotationResetsPeersAndRegistersFromNewPort(t *testing.T) {
	oldTryUPnP := tryLANUPnPFunc
	tryLANUPnPFunc = func(context.Context, *net.UDPConn, string) (*upnp.Mapping, string) { return nil, "" }
	defer func() { tryLANUPnPFunc = oldTryUPnP }()
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	identity, err := lan.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	_, pub, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	peer := &lanP2PPeer{id: "dev-b"}
	peer.connected.Store(true)
	peer.punched.Store(true)
	peer.punching.Store(true)
	peer.isRelay.Store(true)
	peer.openFailures.Store(lanRelayRotateAfter)
	peer.relaySince.Store(time.Now().Add(-2 * lanRelayMaxAge).UnixNano())
	peer.addr.Store(server.LocalAddr().(*net.UDPAddr))
	peer.pc = tunnel.NewPacketConn(client, &peer.addr)
	left, right := net.Pipe()
	defer right.Close()
	peer.kcp = left
	p := &lanP2P{
		conn: client, server: server.LocalAddr().(*net.UDPAddr), deviceID: "dev-a", identity: identity, x25519Pub: pub,
		peers: map[string]*lanP2PPeer{"dev-b": peer}, registering: map[string]bool{"dev-b": true},
		relayTimers: map[string]bool{}, openRetries: map[string]bool{},
	}
	oldPort := client.LocalAddr().(*net.UDPAddr).Port
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p.rotateSocketAndRestartPunch(ctx, "test")

	newConn := p.currentConn()
	if newConn == nil {
		t.Fatal("rotation did not install new socket")
	}
	defer newConn.Close()
	newPort := newConn.LocalAddr().(*net.UDPAddr).Port
	if newPort == oldPort {
		t.Fatalf("expected new local port, still %d", oldPort)
	}
	if peer.connected.Load() || peer.punched.Load() || peer.punching.Load() || peer.isRelay.Load() {
		t.Fatalf("peer state not reset: connected=%v punched=%v punching=%v relay=%v", peer.connected.Load(), peer.punched.Load(), peer.punching.Load(), peer.isRelay.Load())
	}
	if peer.pc != nil || peer.kcp != nil {
		t.Fatalf("peer connection state not cleared: pc=%v kcp=%v", peer.pc, peer.kcp)
	}
	if !p.relayTimers["dev-b"] {
		t.Fatal("relay timer must restart after rotation")
	}

	buf := make([]byte, 2048)
	_ = server.SetReadDeadline(testDeadline())
	n, src, err := server.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if src.Port != newPort {
		t.Fatalf("register must come from new port: got=%d want=%d payload=%s", src.Port, newPort, string(buf[:n]))
	}
	if !strings.Contains(string(buf[:n]), `"t":"lan_register"`) {
		t.Fatalf("expected immediate lan_register after rotation, got %s", string(buf[:n]))
	}
}

func TestLANP2PSocketRotationCooldown(t *testing.T) {
	oldTryUPnP := tryLANUPnPFunc
	tryLANUPnPFunc = func(context.Context, *net.UDPConn, string) (*upnp.Mapping, string) { return nil, "" }
	defer func() { tryLANUPnPFunc = oldTryUPnP }()
	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	p := &lanP2P{conn: client, deviceID: "dev-a"}
	p.lastRotation.Store(time.Now().UnixNano())
	oldPort := client.LocalAddr().(*net.UDPAddr).Port
	p.rotateSocketAndRestartPunch(context.Background(), "test")
	got := p.currentConn()
	if got == nil || got.LocalAddr().(*net.UDPAddr).Port != oldPort {
		t.Fatal("cooldown should keep existing socket")
	}
}

func TestLANP2PRelayFailureDoesNotRotateWhenAnotherPeerReady(t *testing.T) {
	failed := &lanP2PPeer{id: "failed"}
	failed.isRelay.Store(true)
	failed.relaySince.Store(time.Now().Add(-2 * lanRelayMaxAge).UnixNano())
	readyLeft, readyRight := net.Pipe()
	defer readyLeft.Close()
	defer readyRight.Close()
	ready := &lanP2PPeer{id: "ready", kcp: readyLeft}
	ready.connected.Store(true)
	p := &lanP2P{peers: map[string]*lanP2PPeer{"failed": failed, "ready": ready}}

	if p.maybeRotateSocketAfterRelayFailure(context.Background(), failed, "test") {
		t.Fatal("must not rotate socket while another peer has ready KCP")
	}
	if got := failed.openFailures.Load(); got != 1 {
		t.Fatalf("failure count should still increment, got %d", got)
	}
}

func TestLANKCPFrameRoundTrip(t *testing.T) {
	var buf strings.Builder
	if err := writeLANFrame(&buf, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	got, err := readLANFrame(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string([]byte{1, 2, 3}) {
		t.Fatalf("bad frame: %v", got)
	}
}

func TestWaitLANKCPReadyAckRequiresAck(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	done := make(chan error, 1)
	go func() {
		done <- waitLANKCPReadyAck(left)
	}()
	if err := writeLANFrame(right, []byte("not-ready")); err != nil {
		t.Fatal(err)
	}
	err := <-done
	if err == nil || !strings.Contains(err.Error(), "unexpected ready ack") {
		t.Fatalf("expected unexpected ready ack error, got %v", err)
	}
}

func TestWaitLANKCPReadyAckAcceptsListenerAck(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	done := make(chan error, 1)
	go func() {
		done <- waitLANKCPReadyAck(left)
	}()
	if err := writeLANFrame(right, lanKCPReadyFrame); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConfirmLANKCPReadyListenerSendsAck(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	done := make(chan error, 1)
	go func() {
		done <- confirmLANKCPReady(left, true)
	}()
	if _, err := right.Write([]byte("\x00KCP-HELLO\n")); err != nil {
		t.Fatal(err)
	}
	got, err := readLANFrame(right)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(lanKCPReadyFrame) {
		t.Fatalf("bad ready ack: %q", string(got))
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func testDeadline() time.Time {
	return time.Now().Add(2 * time.Second)
}

func udpPair() (*net.UDPConn, *net.UDPConn, error) {
	left, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return nil, nil, err
	}
	right, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		_ = left.Close()
		return nil, nil, err
	}
	return left, right, nil
}
