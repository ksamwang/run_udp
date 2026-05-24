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
			Network:       store.VirtualNetwork{ID: 7, Name: "default", CIDR: "172.16.10.0/24", MTU: 1400, MSS: 1180, PathPolicy: "prefer_p2p", Enabled: true},
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
	if got.DeviceID != "dev-a" || got.PublicKey != "pub" || resp.Network.ID != 7 || resp.Network.MTU != 1400 || resp.Network.MSS != 1180 || resp.Network.PathPolicy != "prefer_p2p" || resp.Address.VirtualIP != "172.16.10.2" || len(resp.Peers) != 1 {
		t.Fatalf("bad bootstrap got=%+v resp=%+v", got, resp)
	}
}

func TestLANNetworkMTUMSS(t *testing.T) {
	network := store.VirtualNetwork{MTU: 1400, MSS: 1180}
	if got := lanNetworkMTU(network); got != 1400 {
		t.Fatalf("mtu=%d", got)
	}
	if got := lanNetworkMSS(network, 1400); got != 1180 {
		t.Fatalf("mss=%d", got)
	}
	defaults := store.VirtualNetwork{}
	if got := lanNetworkMTU(defaults); got != wintun.DefaultMTU {
		t.Fatalf("default mtu=%d", got)
	}
	if got := lanNetworkMSS(defaults, wintun.DefaultMTU); got != vnet.MSSForMTU(wintun.DefaultMTU) {
		t.Fatalf("default mss=%d", got)
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
