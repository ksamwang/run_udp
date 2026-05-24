package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"udp_tunnel_demo/internal/store"
	"udp_tunnel_demo/internal/vnet"
	"udp_tunnel_demo/internal/wintun"
)

func TestRequestLANBootstrap(t *testing.T) {
	var got lanBootstrapRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/lan/bootstrap" {
			t.Fatalf("bad path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(lanBootstrapResponse{
			Version:       1,
			ConfigVersion: "v1",
			STUNAltPort:   7002,
			Network:       store.VirtualNetwork{ID: 7, Name: "default", CIDR: "172.16.10.0/24", MTU: 1400, MSS: 1180, PathPolicy: "prefer_p2p", TCPFastPath: "auto", Enabled: true},
			Address:       store.VirtualAddress{DeviceID: "dev-a", NetworkID: 7, VirtualIP: "172.16.10.2"},
			Peers:         []lanBootstrapPeer{{DeviceID: "dev-b", VirtualIP: "172.16.10.3"}},
		})
	}))
	defer srv.Close()

	resp, err := requestLANBootstrap(context.Background(), srv.URL, lanBootstrapRequest{
		DeviceID: "dev-a", DeviceName: "pc", PublicKey: "pub", Capabilities: []string{"ipv4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != "dev-a" || got.PublicKey != "pub" || resp.Network.ID != 7 || resp.Network.MTU != 1400 || resp.Network.MSS != 1180 || resp.Network.PathPolicy != "prefer_p2p" || resp.Network.TCPFastPath != "auto" || resp.STUNAltPort != 7002 || resp.Address.VirtualIP != "172.16.10.2" || len(resp.Peers) != 1 {
		t.Fatalf("bad bootstrap got=%+v resp=%+v", got, resp)
	}
}

func TestLANNetworkMTUMSS(t *testing.T) {
	tests := []struct {
		name    string
		network store.VirtualNetwork
		wantMTU int
		wantMSS int
	}{
		{
			name:    "explicit mtu mss",
			network: store.VirtualNetwork{MTU: 1400, MSS: 1180},
			wantMTU: 1400,
			wantMSS: 1180,
		},
		{
			name:    "mtu only",
			network: store.VirtualNetwork{MTU: 1280},
			wantMTU: 1280,
			wantMSS: 1200,
		},
		{
			name:    "defaults",
			network: store.VirtualNetwork{},
			wantMTU: wintun.DefaultMTU,
			wantMSS: vnet.MSSForMTU(wintun.DefaultMTU),
		},
		{
			name:    "large mtu clamps mss",
			network: store.VirtualNetwork{MTU: 1500, MSS: 0},
			wantMTU: 1500,
			wantMSS: 1200,
		},
		{
			name:    "small mtu floors mss",
			network: store.VirtualNetwork{MTU: 520},
			wantMTU: 520,
			wantMSS: vnet.MSSForMTU(520),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lanNetworkMTU(tt.network); got != tt.wantMTU {
				t.Fatalf("mtu=%d want=%d", got, tt.wantMTU)
			}
			if got := lanNetworkMSS(tt.network, tt.wantMTU); got != tt.wantMSS {
				t.Fatalf("mss=%d want=%d", got, tt.wantMSS)
			}
		})
	}
}

func TestRequestLANBootstrapRejectsFutureVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(lanBootstrapResponse{Version: lanBootstrapVersion + 1})
	}))
	defer srv.Close()
	if _, err := requestLANBootstrap(context.Background(), srv.URL, lanBootstrapRequest{DeviceID: "dev-a"}); err == nil {
		t.Fatal("expected future version error")
	}
}

func TestReportLANStatus(t *testing.T) {
	var got store.VirtualPeerState
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/lan/status" {
			t.Fatalf("bad path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	err := reportLANStatus(context.Background(), srv.URL, store.VirtualPeerState{
		DeviceID: "dev-a", NetworkID: 7, State: "bootstrap", AdapterState: "not_configured",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != "dev-a" || got.NetworkID != 7 || got.AdapterState != "not_configured" {
		t.Fatalf("bad status: %+v", got)
	}
}

func TestValidateLANRouteSelectionRejectsConflict(t *testing.T) {
	err := validateLANRouteSelection("172.16.10.2", "172.16.10.0/24", wintun.SystemState{
		Conflict: vnet.Conflict{
			CIDR: "172.16.10.0/24", Existing: vnet.Route{CIDR: "172.16.10.0/24", Interface: "vpn"}, Conflicts: true,
		},
		SelectedCIDR: "172.16.11.0/24",
	})
	if err == nil {
		t.Fatal("expected route conflict error")
	}
}

func TestValidateLANRouteSelectionRejectsVirtualIPOutsideCIDR(t *testing.T) {
	err := validateLANRouteSelection("172.16.11.2", "172.16.10.0/24", wintun.SystemState{SelectedCIDR: "172.16.10.0/24"})
	if err == nil {
		t.Fatal("expected virtual IP mismatch error")
	}
}

func TestValidateLANRouteSelectionAcceptsServerCIDR(t *testing.T) {
	if err := validateLANRouteSelection("172.16.10.2", "172.16.10.0/24", wintun.SystemState{SelectedCIDR: "172.16.10.0/24"}); err != nil {
		t.Fatal(err)
	}
}

func TestLANMSSForMTUBoundaries(t *testing.T) {
	tests := []struct {
		name string
		mtu  int
		want int
	}{
		{name: "1500", mtu: 1500, want: 1200},
		{name: "1400", mtu: 1400, want: 1200},
		{name: "1280", mtu: 1280, want: 1200},
		{name: "576", mtu: 576, want: 536},
		{name: "default", mtu: 0, want: vnet.MSSForMTU(wintun.DefaultMTU)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vnet.MSSForMTU(tt.mtu); got != tt.want {
				t.Fatalf("mss=%d want=%d", got, tt.want)
			}
		})
	}
}

func TestLANRouteConflictStatusReportsExistingRoute(t *testing.T) {
	got := lanRouteConflictStatus(wintun.SystemState{
		Conflict: vnet.Conflict{
			CIDR: "172.16.10.0/24", Existing: vnet.Route{CIDR: "172.16.10.0/24", Interface: "vpn"}, Conflicts: true,
		},
		SelectedCIDR: "172.16.11.0/24",
	})
	if got != "172.16.10.0/24 via vpn" {
		t.Fatalf("bad route conflict status: %q", got)
	}
}
