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
	dst, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	src, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	privA, pubA, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	privB, pubB, err := newX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	tx, _, err := lanPeerCodecsFromX25519(privA, pubB, "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	_, rx, err := lanPeerCodecsFromX25519(privB, pubA, "dev-b", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	peer := &lanP2PPeer{id: "dev-b", tx: tx}
	peer.addr.Store(dst.LocalAddr().(*net.UDPAddr))
	peer.punched.Store(true)
	p := &lanP2P{conn: src, deviceID: "dev-a", peers: map[string]*lanP2PPeer{"dev-b": peer}}

	err = p.Send(packet.RoutedFrame{DstDevice: "dev-b", Payload: []byte{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	_ = dst.SetReadDeadline(testDeadline())
	n, _, err := dst.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := rx.Open(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string([]byte{1, 2, 3}) {
		t.Fatalf("bad packet data=%v", plain)
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
		peers: map[string]*lanP2PPeer{}, registering: map[string]bool{}, x25519Pub: pub,
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
}

func testDeadline() time.Time {
	return time.Now().Add(2 * time.Second)
}
