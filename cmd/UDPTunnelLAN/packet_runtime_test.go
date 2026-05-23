package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
