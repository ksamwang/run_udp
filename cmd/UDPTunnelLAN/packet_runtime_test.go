package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"udp_tunnel_demo/internal/lan"
	"udp_tunnel_demo/internal/packet"
	"udp_tunnel_demo/internal/protocol"
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

func TestLANP2PIgnoresPeerInfoInRelayMode(t *testing.T) {
	relayAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7000}
	p := &lanP2P{
		deviceID: "dev-a",
		peers: map[string]*lanP2PPeer{
			"dev-b": {id: "dev-b"},
		},
	}
	peer := p.peers["dev-b"]
	peer.addr.Store(relayAddr)
	peer.isRelay.Store(true)
	msg := &protocol.Message{Type: protocol.MsgPeerInfo, Peer: "dev-b", Profile: "lan-packet", Addr: "203.0.113.10:40000"}
	b, _ := protocol.Encode(msg)
	p.handleControl(context.Background(), b, relayAddr, nil, nil, packet.NewLinkManager(packet.LinkConfig{DeviceID: "dev-a"}))
	if got := peer.addr.Load(); got == nil || got.String() != relayAddr.String() {
		t.Fatalf("relay addr overwritten by peer info: %v", got)
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

func testDeadline() time.Time {
	return time.Now().Add(2 * time.Second)
}
