package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"udp_tunnel_demo/internal/config"
	"udp_tunnel_demo/internal/lan"
	"udp_tunnel_demo/internal/lantransport"
	"udp_tunnel_demo/internal/protocol"
	"udp_tunnel_demo/internal/secure"
	"udp_tunnel_demo/internal/store"
)

func TestHandleForwardsValidationErrors(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", false)

	tests := []struct {
		name string
		body map[string]any
		code string
	}{
		{
			name: "missing source device",
			body: map[string]any{"name": "rdp", "source_id": "missing", "target_id": "dev-a", "local_port": 11388, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true},
			code: "device_not_found",
		},
		{
			name: "disabled target device",
			body: map[string]any{"name": "rdp", "source_id": "dev-a", "target_id": "dev-b", "local_port": 11388, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true},
			code: "device_disabled",
		},
		{
			name: "same device forbidden",
			body: map[string]any{"name": "rdp", "source_id": "dev-a", "target_id": "dev-a", "local_port": 11388, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true},
			code: "same_device_forbidden",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/rules", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp["code"] != tc.code {
				t.Fatalf("expected code %q got %v body=%s", tc.code, resp["code"], rec.Body.String())
			}
		})
	}
}

func TestCORSAllowsAnyOriginAndHeaders(t *testing.T) {
	a := newTestApp(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/admin/auth/login", nil)
	req.Header.Set("Origin", "https://admin.tunnel.wanglv.top")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type,x-custom-header")
	a.httpMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow origin=%q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "*" {
		t.Fatalf("allow headers=%q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("allow methods should be set")
	}
}

func TestLANAdminAPIs(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)

	unauth := doJSON(t, a.httpMux(), http.MethodGet, "/api/admin/lan/networks", nil, nil)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauth.Code, unauth.Body.String())
	}

	createNet := doAdminJSON(t, a, http.MethodPost, "/api/admin/lan/networks", map[string]any{
		"name": "office", "cidr": "172.16.30.0/24", "mtu": 1400, "mss": 1200, "path_policy": "prefer_p2p", "tcp_fast_path": "auto", "enabled": true,
	})
	if createNet.Code != http.StatusOK {
		t.Fatalf("create network status=%d body=%s", createNet.Code, createNet.Body.String())
	}
	var network store.VirtualNetwork
	if err := json.Unmarshal(createNet.Body.Bytes(), &network); err != nil {
		t.Fatal(err)
	}
	if network.ID == 0 || network.CIDR != "172.16.30.0/24" || network.MTU != 1400 || network.MSS != 1200 || network.PathPolicy != "prefer_p2p" || network.TCPFastPath != "auto" {
		t.Fatalf("bad network: %+v", network)
	}

	patchNet := doAdminJSON(t, a, http.MethodPatch, "/api/admin/lan/networks/"+strconv.FormatInt(network.ID, 10), map[string]any{
		"name": "office-2", "cidr": "172.16.31.0/24", "mtu": 1280, "mss": 1180, "path_policy": "auto", "tcp_fast_path": "force", "enabled": false,
	})
	if patchNet.Code != http.StatusOK {
		t.Fatalf("patch network status=%d body=%s", patchNet.Code, patchNet.Body.String())
	}
	networks, err := a.db.ListVirtualNetworks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range networks {
		if item.ID == network.ID {
			network = item
			break
		}
	}
	if network.PathPolicy != "auto" || network.TCPFastPath != "force" {
		t.Fatalf("bad patched network policy: %+v", network)
	}
	emptyNet := doAdminJSON(t, a, http.MethodPost, "/api/admin/lan/networks", map[string]any{
		"name": "empty", "cidr": "172.16.32.0/24", "enabled": true,
	})
	if emptyNet.Code != http.StatusOK {
		t.Fatalf("create empty network status=%d body=%s", emptyNet.Code, emptyNet.Body.String())
	}
	var emptyNetwork store.VirtualNetwork
	if err := json.Unmarshal(emptyNet.Body.Bytes(), &emptyNetwork); err != nil {
		t.Fatal(err)
	}
	deleteEmpty := doAdminJSON(t, a, http.MethodDelete, "/api/admin/lan/networks/"+strconv.FormatInt(emptyNetwork.ID, 10), nil)
	if deleteEmpty.Code != http.StatusOK {
		t.Fatalf("delete empty network status=%d body=%s", deleteEmpty.Code, deleteEmpty.Body.String())
	}

	addrRec := doAdminJSON(t, a, http.MethodPatch, "/api/admin/lan/addresses/dev-a", map[string]any{
		"network_id": network.ID, "virtual_ip": "172.16.31.10", "hostname": "office-a", "dns_enabled": true,
	})
	if addrRec.Code != http.StatusOK {
		t.Fatalf("patch address status=%d body=%s", addrRec.Code, addrRec.Body.String())
	}
	badAddr := doAdminJSON(t, a, http.MethodPatch, "/api/admin/lan/addresses/dev-b", map[string]any{
		"network_id": network.ID, "virtual_ip": "192.168.1.10", "hostname": "bad",
	})
	if badAddr.Code != http.StatusBadRequest {
		t.Fatalf("bad address status=%d body=%s", badAddr.Code, badAddr.Body.String())
	}
	networkAddr := doAdminJSON(t, a, http.MethodPatch, "/api/admin/lan/addresses/dev-b", map[string]any{
		"network_id": network.ID, "virtual_ip": "172.16.31.0", "hostname": "bad",
	})
	if networkAddr.Code != http.StatusBadRequest {
		t.Fatalf("network address status=%d body=%s", networkAddr.Code, networkAddr.Body.String())
	}
	broadcastAddr := doAdminJSON(t, a, http.MethodPatch, "/api/admin/lan/addresses/dev-b", map[string]any{
		"network_id": network.ID, "virtual_ip": "172.16.31.255", "hostname": "bad",
	})
	if broadcastAddr.Code != http.StatusBadRequest {
		t.Fatalf("broadcast address status=%d body=%s", broadcastAddr.Code, broadcastAddr.Body.String())
	}
	listAddr := doAdminJSON(t, a, http.MethodGet, "/api/admin/lan/addresses?network_id="+strconv.FormatInt(network.ID, 10), nil)
	if listAddr.Code != http.StatusOK {
		t.Fatalf("list address status=%d body=%s", listAddr.Code, listAddr.Body.String())
	}
	var addresses []store.VirtualAddress
	if err := json.Unmarshal(listAddr.Body.Bytes(), &addresses); err != nil {
		t.Fatal(err)
	}
	if len(addresses) != 1 || addresses[0].VirtualIP != "172.16.31.10" {
		t.Fatalf("bad addresses: %+v", addresses)
	}
	bootstrapAddr := doAdminJSON(t, a, http.MethodPost, "/api/admin/lan/addresses/dev-a/bootstrap?network_id="+strconv.FormatInt(network.ID, 10), nil)
	if bootstrapAddr.Code != http.StatusOK {
		t.Fatalf("bootstrap address status=%d body=%s", bootstrapAddr.Code, bootstrapAddr.Body.String())
	}
	reassignAddr := doAdminJSON(t, a, http.MethodPost, "/api/admin/lan/addresses/dev-a/reassign?network_id="+strconv.FormatInt(network.ID, 10), nil)
	if reassignAddr.Code != http.StatusOK {
		t.Fatalf("reassign address status=%d body=%s", reassignAddr.Code, reassignAddr.Body.String())
	}
	addr, err := a.db.GetVirtualAddress(ctx, network.ID, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if addr.VirtualIP == "" || addr.VirtualIP == "172.16.31.10" {
		t.Fatalf("bad reassigned address: %+v", addr)
	}
	releaseAddr := doAdminJSON(t, a, http.MethodPost, "/api/admin/lan/addresses/dev-a/release?network_id="+strconv.FormatInt(network.ID, 10), nil)
	if releaseAddr.Code != http.StatusOK {
		t.Fatalf("release address status=%d body=%s", releaseAddr.Code, releaseAddr.Body.String())
	}
	if _, err := a.db.GetVirtualAddress(ctx, network.ID, "dev-a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("address should be released, err=%v", err)
	}
	if _, err := a.allocateVirtualAddress(ctx, network, "dev-a", "office-a"); err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualDeviceKey(ctx, store.VirtualDeviceKey{DeviceID: "dev-a", Algorithm: "ed25519", PublicKey: "pub-a"}); err != nil {
		t.Fatal(err)
	}
	keysRec := doAdminJSON(t, a, http.MethodGet, "/api/admin/lan/device-keys", nil)
	if keysRec.Code != http.StatusOK {
		t.Fatalf("list device keys status=%d body=%s", keysRec.Code, keysRec.Body.String())
	}
	var keys []store.VirtualDeviceKey
	if err := json.Unmarshal(keysRec.Body.Bytes(), &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].DeviceID != "dev-a" || keys[0].PublicKey != "pub-a" {
		t.Fatalf("bad device keys: %+v", keys)
	}
	groupRec := doAdminJSON(t, a, http.MethodPost, "/api/admin/lan/groups", map[string]any{
		"id": "ops", "name": "Ops", "device_ids": []string{"dev-a", "dev-b"},
	})
	if groupRec.Code != http.StatusOK {
		t.Fatalf("create group status=%d body=%s", groupRec.Code, groupRec.Body.String())
	}
	listGroups := doAdminJSON(t, a, http.MethodGet, "/api/admin/lan/groups", nil)
	if listGroups.Code != http.StatusOK {
		t.Fatalf("list groups status=%d body=%s", listGroups.Code, listGroups.Body.String())
	}
	var groups []lanGroupResponse
	if err := json.Unmarshal(listGroups.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != "ops" || len(groups[0].DeviceIDs) != 2 {
		t.Fatalf("bad groups: %+v", groups)
	}
	patchGroup := doAdminJSON(t, a, http.MethodPatch, "/api/admin/lan/groups/ops", map[string]any{
		"id": "ignored", "name": "Ops Team", "device_ids": []string{"dev-a"},
	})
	if patchGroup.Code != http.StatusOK {
		t.Fatalf("patch group status=%d body=%s", patchGroup.Code, patchGroup.Body.String())
	}
	delGroup := doAdminJSON(t, a, http.MethodDelete, "/api/admin/lan/groups/ops", nil)
	if delGroup.Code != http.StatusOK {
		t.Fatalf("delete group status=%d body=%s", delGroup.Code, delGroup.Body.String())
	}
	deleteInUse := doAdminJSON(t, a, http.MethodDelete, "/api/admin/lan/networks/"+strconv.FormatInt(network.ID, 10), nil)
	if deleteInUse.Code != http.StatusBadRequest {
		t.Fatalf("delete in-use network status=%d body=%s", deleteInUse.Code, deleteInUse.Body.String())
	}

	aclRec := doAdminJSON(t, a, http.MethodPost, "/api/admin/lan/acl", map[string]any{
		"network_id": network.ID, "source_device_id": "dev-a", "target_device_id": "dev-b",
		"protocol": "tcp", "port_start": 3389, "port_end": 3389, "action": "deny", "enabled": true,
	})
	if aclRec.Code != http.StatusOK {
		t.Fatalf("create acl status=%d body=%s", aclRec.Code, aclRec.Body.String())
	}
	var acl store.VirtualACLRule
	if err := json.Unmarshal(aclRec.Body.Bytes(), &acl); err != nil {
		t.Fatal(err)
	}
	if acl.ID == 0 || acl.Action != "deny" {
		t.Fatalf("bad acl: %+v", acl)
	}
	patchACL := doAdminJSON(t, a, http.MethodPatch, "/api/admin/lan/acl/"+strconv.FormatInt(acl.ID, 10), map[string]any{
		"network_id": network.ID, "source_device_id": "dev-a", "target_device_id": "dev-b",
		"protocol": "tcp", "port_start": 22, "port_end": 22, "action": "allow", "enabled": false,
	})
	if patchACL.Code != http.StatusOK {
		t.Fatalf("patch acl status=%d body=%s", patchACL.Code, patchACL.Body.String())
	}
	delACL := doAdminJSON(t, a, http.MethodDelete, "/api/admin/lan/acl/"+strconv.FormatInt(acl.ID, 10), nil)
	if delACL.Code != http.StatusOK {
		t.Fatalf("delete acl status=%d body=%s", delACL.Code, delACL.Body.String())
	}

	routeRec := doAdminJSON(t, a, http.MethodPost, "/api/admin/lan/routes", map[string]any{
		"network_id": network.ID, "device_id": "dev-a", "cidr": "10.10.0.0/16", "advertise": true, "accept": false,
	})
	if routeRec.Code != http.StatusOK {
		t.Fatalf("create route status=%d body=%s", routeRec.Code, routeRec.Body.String())
	}
	var route store.VirtualRoute
	if err := json.Unmarshal(routeRec.Body.Bytes(), &route); err != nil {
		t.Fatal(err)
	}
	if route.ID == 0 || route.CIDR != "10.10.0.0/16" || !route.Advertise || route.Accept {
		t.Fatalf("bad route: %+v", route)
	}
	listRoutes := doAdminJSON(t, a, http.MethodGet, "/api/admin/lan/routes?network_id="+strconv.FormatInt(network.ID, 10)+"&device_id=dev-a", nil)
	if listRoutes.Code != http.StatusOK {
		t.Fatalf("list routes status=%d body=%s", listRoutes.Code, listRoutes.Body.String())
	}
	var routes []store.VirtualRoute
	if err := json.Unmarshal(listRoutes.Body.Bytes(), &routes); err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].ID != route.ID {
		t.Fatalf("bad routes: %+v", routes)
	}
	patchRoute := doAdminJSON(t, a, http.MethodPatch, "/api/admin/lan/routes/"+strconv.FormatInt(route.ID, 10), map[string]any{
		"network_id": network.ID, "device_id": "dev-a", "cidr": "10.20.0.0/16", "advertise": false, "accept": true,
	})
	if patchRoute.Code != http.StatusOK {
		t.Fatalf("patch route status=%d body=%s", patchRoute.Code, patchRoute.Body.String())
	}
	routes, err = a.db.ListVirtualRoutes(ctx, network.ID, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].CIDR != "10.20.0.0/16" || routes[0].Advertise || !routes[0].Accept {
		t.Fatalf("bad patched routes: %+v", routes)
	}
	badRoute := doAdminJSON(t, a, http.MethodPost, "/api/admin/lan/routes", map[string]any{
		"network_id": network.ID, "device_id": "dev-a", "cidr": "0.0.0.0/0", "advertise": true, "accept": true,
	})
	if badRoute.Code != http.StatusBadRequest {
		t.Fatalf("bad route status=%d body=%s", badRoute.Code, badRoute.Body.String())
	}
	delRoute := doAdminJSON(t, a, http.MethodDelete, "/api/admin/lan/routes/"+strconv.FormatInt(route.ID, 10), nil)
	if delRoute.Code != http.StatusOK {
		t.Fatalf("delete route status=%d body=%s", delRoute.Code, delRoute.Body.String())
	}

	badNet := doAdminJSON(t, a, http.MethodPost, "/api/admin/lan/networks", map[string]any{
		"name": "bad", "cidr": "not-cidr", "enabled": true,
	})
	if badNet.Code != http.StatusBadRequest {
		t.Fatalf("bad network status=%d body=%s", badNet.Code, badNet.Body.String())
	}
}

func TestLANBootstrapAndStatusAPIs(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	network, err := a.db.EnsureDefaultVirtualNetwork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualAddress(ctx, store.VirtualAddress{
		DeviceID: "dev-a", NetworkID: network.ID, VirtualIP: "172.16.10.10", Hostname: "office-a",
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualAddress(ctx, store.VirtualAddress{
		DeviceID: "dev-b", NetworkID: network.ID, VirtualIP: "172.16.10.11", Hostname: "office-b",
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualDeviceKey(ctx, store.VirtualDeviceKey{
		DeviceID: "dev-b", Algorithm: "ed25519", PublicKey: "pub-b",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.CreateVirtualACLRule(ctx, store.VirtualACLRule{
		NetworkID: network.ID, SourceDeviceID: "dev-a", TargetDeviceID: "dev-b",
		Protocol: "tcp", PortStart: 3389, PortEnd: 3389, Action: "allow", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.UpsertVirtualDeviceGroup(ctx, store.VirtualDeviceGroup{ID: "ops", Name: "Ops"}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetVirtualDeviceGroupMembers(ctx, "ops", []string{"dev-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.CreateVirtualACLRule(ctx, store.VirtualACLRule{
		NetworkID: network.ID, SourceGroupID: "ops", TargetDeviceID: "dev-b",
		Protocol: "tcp", PortStart: 22, PortEnd: 22, Action: "deny", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualRoute(ctx, store.VirtualRoute{
		DeviceID: "dev-a", NetworkID: network.ID, CIDR: "10.30.0.0/16", Advertise: true, Accept: true,
	}); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/bootstrap", map[string]any{
		"device_id": "dev-a", "device_name": "Office A", "public_key": "pub-a", "capabilities": []string{"ipv4"},
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp lanBootstrapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Version != lanBootstrapVersion || resp.ConfigVersion == "" || resp.Address.VirtualIP != "172.16.10.10" {
		t.Fatalf("bad bootstrap response: %+v", resp)
	}
	if resp.Server == "" {
		t.Fatalf("bootstrap should return UDP rendezvous server: %+v", resp)
	}
	if resp.STUNAltPort == 0 {
		t.Fatalf("bootstrap should return STUN alt port: %+v", resp)
	}
	if len(resp.Peers) != 1 || resp.Peers[0].DeviceID != "dev-b" {
		t.Fatalf("bad bootstrap peers: %+v", resp.Peers)
	}
	if resp.Peers[0].PublicKey != "pub-b" {
		t.Fatalf("peer public key not returned: %+v", resp.Peers)
	}
	if len(resp.Routes) != 1 || resp.Routes[0].CIDR != "10.30.0.0/16" {
		t.Fatalf("bootstrap routes not returned: %+v", resp.Routes)
	}
	hasExpandedGroupACL := false
	for _, rule := range resp.ACL {
		if rule.SourceDeviceID == "dev-a" && rule.TargetDeviceID == "dev-b" && rule.SourceGroupID == "" && rule.PortStart == 22 {
			hasExpandedGroupACL = true
		}
	}
	if !hasExpandedGroupACL {
		t.Fatalf("group acl not expanded: %+v", resp.ACL)
	}
	key, err := a.db.GetVirtualDeviceKey(ctx, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if key.Algorithm != "ed25519" || key.PublicKey != "pub-a" {
		t.Fatalf("bootstrap public key not stored: %+v", key)
	}

	statusRec := doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/status", map[string]any{
		"device_id": "dev-a", "peer_id": "dev-b", "network_id": network.ID,
		"state": "connected", "path": "p2p", "data_path": "p2p_datagram", "path_reason": "datagram_ready", "traffic_class": "interactive",
		"adapter_state": "up", "route_conflict": "none",
		"selected_cidr": "172.16.10.0/24", "mtu": 1280, "mss": 1200,
		"rtt_ms": 12, "estimated_bps": 300, "tx_bytes": 100, "rx_bytes": 200,
	}, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}
	statusRec = doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/status", map[string]any{
		"device_id": "dev-a", "peer_id": "dev-b", "network_id": network.ID,
		"state": "connected", "path": "relay", "data_path": "relay_udp", "path_reason": "p2p_timeout", "traffic_class": "throughput",
		"nat_type": "relay_required", "fallback_reason": "p2p_timeout", "tcp_fast_path": "auto_candidate",
		"adapter_state": "up", "selected_cidr": "172.16.10.0/24", "mtu": 1280, "mss": 1200,
		"tx_bytes": 300, "rx_bytes": 400,
	}, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("second status status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}
	statesRec := doAdminJSON(t, a, http.MethodGet, "/api/admin/lan/peer-states?network_id="+strconv.FormatInt(network.ID, 10), nil)
	if statesRec.Code != http.StatusOK {
		t.Fatalf("admin lan states status=%d body=%s", statesRec.Code, statesRec.Body.String())
	}
	var adminStates []store.VirtualPeerState
	if err := json.Unmarshal(statesRec.Body.Bytes(), &adminStates); err != nil {
		t.Fatal(err)
	}
	if len(adminStates) != 1 || adminStates[0].DeviceID != "dev-a" || adminStates[0].PeerID != "dev-b" {
		t.Fatalf("bad admin lan states: %+v", adminStates)
	}
	states, err := a.db.ListVirtualPeerStates(ctx, network.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Path != "relay" ||
		states[0].AdapterState != "up" || states[0].MSS != 1200 || states[0].DataPath != "relay_udp" ||
		states[0].PathReason != "p2p_timeout" || states[0].NATType != "relay_required" ||
		states[0].FallbackReason != "p2p_timeout" || states[0].TrafficClass != "throughput" ||
		states[0].TCPFastPath != "auto_candidate" || states[0].TxBytes != 300 {
		t.Fatalf("bad peer states: %+v", states)
	}
	eventsRec := doAdminJSON(t, a, http.MethodGet, "/api/admin/lan/path-events?network_id="+strconv.FormatInt(network.ID, 10), nil)
	if eventsRec.Code != http.StatusOK {
		t.Fatalf("admin lan path events status=%d body=%s", eventsRec.Code, eventsRec.Body.String())
	}
	var pathEvents []store.VirtualPeerPathEvent
	if err := json.Unmarshal(eventsRec.Body.Bytes(), &pathEvents); err != nil {
		t.Fatal(err)
	}
	if len(pathEvents) != 2 || pathEvents[0].DataPath != "relay_udp" || pathEvents[1].DataPath != "p2p_datagram" {
		t.Fatalf("bad path events: %+v", pathEvents)
	}
	diagRec := doAdminJSON(t, a, http.MethodGet, "/api/admin/lan/diagnostics?network_id="+strconv.FormatInt(network.ID, 10), nil)
	if diagRec.Code != http.StatusOK {
		t.Fatalf("admin lan diagnostics status=%d body=%s", diagRec.Code, diagRec.Body.String())
	}
	var diag lanDiagnosticsSnapshot
	if err := json.Unmarshal(diagRec.Body.Bytes(), &diag); err != nil {
		t.Fatal(err)
	}
	if diag.NetworkID != network.ID || len(diag.Networks) == 0 || len(diag.Addresses) == 0 ||
		len(diag.PeerStates) != 1 || len(diag.PathEvents) != 2 {
		t.Fatalf("bad diagnostics snapshot: %+v", diag)
	}
	deviceStatesRec := doAdminJSON(t, a, http.MethodGet, "/api/admin/lan/device-states?network_id="+strconv.FormatInt(network.ID, 10), nil)
	if deviceStatesRec.Code != http.StatusOK {
		t.Fatalf("admin lan device states status=%d body=%s", deviceStatesRec.Code, deviceStatesRec.Body.String())
	}
	var deviceStates []lanDeviceState
	if err := json.Unmarshal(deviceStatesRec.Body.Bytes(), &deviceStates); err != nil {
		t.Fatal(err)
	}
	var devAState lanDeviceState
	for _, state := range deviceStates {
		if state.DeviceID == "dev-a" {
			devAState = state
			break
		}
	}
	if devAState.DeviceID != "dev-a" || devAState.VirtualIP != "172.16.10.10" ||
		devAState.AdapterState != "up" || devAState.RelayPeers != 1 {
		t.Fatalf("bad lan device states: %+v", deviceStates)
	}
}

func TestLANBootstrapUsesAssignedNetwork(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if _, err := a.db.EnsureDefaultVirtualNetwork(ctx); err != nil {
		t.Fatal(err)
	}
	network, err := a.db.CreateVirtualNetwork(ctx, store.VirtualNetwork{Name: "branch", CIDR: "172.16.40.0/24", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualAddress(ctx, store.VirtualAddress{
		DeviceID: "dev-branch", NetworkID: network.ID, VirtualIP: "172.16.40.10", Hostname: "branch",
	}); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/bootstrap", map[string]any{
		"device_id": "dev-branch", "device_name": "Branch",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp lanBootstrapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Network.ID != network.ID || resp.Network.CIDR != "172.16.40.0/24" || resp.Address.VirtualIP != "172.16.40.10" {
		t.Fatalf("bootstrap did not use assigned network: %+v", resp)
	}
}

func TestLANBootstrapAutoAssignsVirtualIP(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	network, err := a.db.EnsureDefaultVirtualNetwork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualAddress(ctx, store.VirtualAddress{
		DeviceID: "used", NetworkID: network.ID, VirtualIP: "172.16.10.1",
	}); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/bootstrap", map[string]any{
		"device_id": "dev-new", "device_name": "Office New", "public_key": "pub-new",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp lanBootstrapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Address.DeviceID != "dev-new" || resp.Address.VirtualIP != "172.16.10.2" {
		t.Fatalf("bad auto assigned address: %+v", resp.Address)
	}
}

func TestLANBootstrapAutoAssignsWhenHostnameConflicts(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	network, err := a.db.EnsureDefaultVirtualNetwork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualAddress(ctx, store.VirtualAddress{
		DeviceID: "dev-a", NetworkID: network.ID, VirtualIP: "172.16.10.2", Hostname: "same-host",
	}); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/bootstrap", map[string]any{
		"device_id": "dev-b", "device_name": "same-host",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp lanBootstrapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Address.DeviceID != "dev-b" || resp.Address.VirtualIP != "172.16.10.1" || resp.Address.Hostname != "dev-b" {
		t.Fatalf("bad auto assigned address: %+v", resp.Address)
	}
}

func TestLANBootstrapRepairsEmptyVirtualIP(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	network, err := a.db.EnsureDefaultVirtualNetwork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualAddress(ctx, store.VirtualAddress{
		DeviceID: "dev-empty", NetworkID: network.ID, VirtualIP: "172.16.10.1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualAddress(ctx, store.VirtualAddress{
		DeviceID: "dev-empty", NetworkID: network.ID, VirtualIP: "",
	}); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/bootstrap", map[string]any{
		"device_id": "dev-empty", "device_name": "Repair Me",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp lanBootstrapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Address.VirtualIP == "" {
		t.Fatalf("expected repaired virtual ip: %+v", resp.Address)
	}
}

func TestLANPacketRelayDisabledByDefault(t *testing.T) {
	a := newTestApp(t)
	a.cfg.AllowRelay = false
	a.cfg.LANAllowRelay = false
	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/packets/send", map[string]any{
		"device_id": "dev-a",
		"frames": []map[string]any{{
			"network_id": 1, "src_device": "dev-a", "dst_device": "dev-b", "type": 1, "payload": "AQID",
		}},
	}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "relay_disabled" {
		t.Fatalf("expected relay_disabled, got %v body=%s", resp["code"], rec.Body.String())
	}
}

func TestLANPacketRelaySendAndPoll(t *testing.T) {
	a := newTestApp(t)
	a.cfg.AllowRelay = false
	a.cfg.LANAllowRelay = true
	send := doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/packets/send", map[string]any{
		"device_id": "dev-a",
		"frames": []map[string]any{{
			"network_id": 1, "src_device": "dev-a", "dst_device": "dev-b", "type": 1, "payload": "AQID",
		}},
	}, nil)
	if send.Code != http.StatusOK {
		t.Fatalf("send status=%d body=%s", send.Code, send.Body.String())
	}
	var sendResp map[string]int
	if err := json.Unmarshal(send.Body.Bytes(), &sendResp); err != nil {
		t.Fatal(err)
	}
	if sendResp["accepted"] != 1 {
		t.Fatalf("bad send response: %s", send.Body.String())
	}

	poll := doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/packets/poll", map[string]any{
		"device_id": "dev-b", "max": 10,
	}, nil)
	if poll.Code != http.StatusOK {
		t.Fatalf("poll status=%d body=%s", poll.Code, poll.Body.String())
	}
	var pollResp struct {
		Frames []lanPacketRelayFrame `json:"frames"`
	}
	if err := json.Unmarshal(poll.Body.Bytes(), &pollResp); err != nil {
		t.Fatal(err)
	}
	if len(pollResp.Frames) != 1 || pollResp.Frames[0].SrcDevice != "dev-a" || pollResp.Frames[0].Payload != "AQID" {
		t.Fatalf("bad poll response: %+v", pollResp.Frames)
	}
}

func TestLANPacketRelayUsesLANSpecificSwitch(t *testing.T) {
	a := newTestApp(t)
	a.cfg.AllowRelay = true
	a.cfg.LANAllowRelay = false
	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/lan/packets/send", map[string]any{
		"device_id": "dev-a",
		"frames": []map[string]any{{
			"network_id": 1, "src_device": "dev-a", "dst_device": "dev-b", "type": 1, "payload": "AQID",
		}},
	}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("lan relay should ignore allow_relay and use lan_allow_relay, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLANUDPRelayForwardsCiphertextByDevice(t *testing.T) {
	a := newTestApp(t)
	a.cfg.LANAllowRelay = true
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 21001}
	dstConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer dstConn.Close()
	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer serverConn.Close()
	a.lanPeers = map[string]map[string]*peer{
		"dev-b": {
			peerSlotKey("dev-a", store.ProfileLANPacket): {
				id: "dev-b", addr: dstConn.LocalAddr().(*net.UDPAddr), want: "dev-a", profile: store.ProfileLANPacket,
			},
		},
	}
	wire, err := lantransport.PackRelayFrame(lantransport.RelayFrame{SrcDevice: "dev-a", DstDevice: "dev-b", Payload: []byte("ciphertext")})
	if err != nil {
		t.Fatal(err)
	}
	if !a.handleLANUDPRelay(serverConn, src, wire) {
		t.Fatal("LAN UDP relay frame should be handled")
	}
	buf := make([]byte, 1024)
	_ = dstConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := dstConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	got, err := lantransport.UnpackRelayFrame(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if got.SrcDevice != "dev-a" || got.DstDevice != "dev-b" || string(got.Payload) != "ciphertext" {
		t.Fatalf("server must forward opaque ciphertext, got %+v", got)
	}
}

func TestHandleForwardsLocalPortConflict(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	mustUpsertDevice(t, a, ctx, "dev-c", true)
	if _, err := a.db.CreateRule(ctx, store.ForwardRule{
		Name: "rdp-1", SourceID: "dev-a", TargetID: "dev-b", LocalPort: 11388,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/rules", map[string]any{
		"name": "rdp-2", "source_id": "dev-a", "target_id": "dev-c", "local_port": 11388,
		"target_host": "127.0.0.1", "target_port": 3389, "enabled": true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "local_port_conflict" {
		t.Fatalf("expected local_port_conflict got %v body=%s", resp["code"], rec.Body.String())
	}
}

func TestHandleForwardsProfileValidationAndBulkRule(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)

	rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/rules", map[string]any{
		"name": "smb", "source_id": "dev-a", "target_id": "dev-b", "profile": "bulk", "local_port": 1445,
		"target_host": "127.0.0.1", "target_port": 445, "enabled": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rule store.ForwardRule
	if err := json.Unmarshal(rec.Body.Bytes(), &rule); err != nil {
		t.Fatal(err)
	}
	if rule.Profile != store.ProfileBulk {
		t.Fatalf("expected bulk profile: %+v", rule)
	}

	rec = doAdminJSON(t, a, http.MethodPost, "/api/admin/rules", map[string]any{
		"name": "bad", "source_id": "dev-a", "target_id": "dev-b", "profile": "video", "local_port": 1446,
		"target_host": "127.0.0.1", "target_port": 445, "enabled": true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEnrichedRulesMatchesProfileTunnelState(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	if _, err := a.db.CreateRule(ctx, store.ForwardRule{
		Name: "rdp", SourceID: "dev-a", TargetID: "dev-b", Profile: store.ProfileInteractive, LocalPort: 13389,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.CreateRule(ctx, store.ForwardRule{
		Name: "smb", SourceID: "dev-a", TargetID: "dev-b", Profile: store.ProfileBulk, LocalPort: 1445,
		TargetHost: "127.0.0.1", TargetPort: 445, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutTunnelState(ctx, store.TunnelState{
		DeviceID: "dev-a", PeerID: "dev-b", Profile: store.ProfileBulk, State: "p2p", Via: "p2p",
	}); err != nil {
		t.Fatal(err)
	}
	rules, err := a.enrichedRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stateByName := map[string]string{}
	for _, r := range rules {
		stateByName[r.Name] = r.RuntimeState
	}
	if stateByName["smb"] != "p2p" || stateByName["rdp"] != "down" {
		t.Fatalf("unexpected runtime states: %+v", stateByName)
	}
}

func TestRegisterPairsSameDevicesByProfile(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	a.handleRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 11001}, &protocol.Message{From: "dev-a", Peer: "dev-b", Profile: store.ProfileInteractive})
	a.handleRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 11002}, &protocol.Message{From: "dev-b", Peer: "dev-a", Profile: store.ProfileInteractive})
	a.handleRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12001}, &protocol.Message{From: "dev-a", Peer: "dev-b", Profile: store.ProfileBulk})
	a.handleRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12002}, &protocol.Message{From: "dev-b", Peer: "dev-a", Profile: store.ProfileBulk})
	if len(a.pairByID) != 2 {
		t.Fatalf("expected separate profile pair sessions, got %+v", a.pairByID)
	}
	if _, ok := a.peers["dev-a"][peerSlotKey("dev-b", store.ProfileInteractive)]; !ok {
		t.Fatalf("missing interactive peer slot: %+v", a.peers)
	}
	if _, ok := a.peers["dev-a"][peerSlotKey("dev-b", store.ProfileBulk)]; !ok {
		t.Fatalf("missing bulk peer slot: %+v", a.peers)
	}
}

func TestRegisterSupportsLANPacketProfile(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	a.handleRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 13001}, &protocol.Message{From: "dev-a", Peer: "dev-b", Profile: store.ProfileLANPacket})
	a.handleRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 13002}, &protocol.Message{From: "dev-b", Peer: "dev-a", Profile: store.ProfileLANPacket})
	if _, ok := a.peers["dev-a"][peerSlotKey("dev-b", store.ProfileLANPacket)]; !ok {
		t.Fatalf("missing lan packet peer slot: %+v", a.peers)
	}
	if _, ok := a.pairByID[pairKey("dev-a", "dev-b", store.ProfileLANPacket)]; !ok {
		t.Fatalf("missing lan packet pair session: %+v", a.pairByID)
	}
}

func TestLANRegisterRequiresValidIdentitySignature(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	idA, err := lan.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualDeviceKey(ctx, store.VirtualDeviceKey{DeviceID: "dev-a", Algorithm: lan.IdentityKeyAlgorithm, PublicKey: idA.PublicKey}); err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 14001}
	a.handleLANRegister(conn, src, &protocol.Message{Type: protocol.MsgLANRegister, From: "dev-a", Peer: "dev-b", Profile: store.ProfileLANPacket})
	if len(a.lanPeers) != 0 {
		t.Fatalf("unsigned LAN register should be rejected: %+v", a.lanPeers)
	}
	ts := time.Now().Unix()
	payloadA := "x25519-a"
	a.handleLANRegister(conn, src, &protocol.Message{
		Type: protocol.MsgLANRegister, From: "dev-a", Peer: "dev-b", Profile: store.ProfileLANPacket,
		Payload: payloadA, Timestamp: ts, Signature: mustSignRegisterPayload(t, idA, "dev-a", "dev-b", store.ProfileLANPacket, ts, payloadA),
	})
	if _, ok := a.lanPeers["dev-a"][peerSlotKey("dev-b", store.ProfileLANPacket)]; !ok {
		t.Fatalf("signed LAN register should be stored: %+v", a.lanPeers)
	}
}

func TestLANRegisterPairsPeers(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	idA, _ := lan.GenerateIdentity()
	idB, _ := lan.GenerateIdentity()
	if err := a.db.UpsertVirtualDeviceKey(ctx, store.VirtualDeviceKey{DeviceID: "dev-a", Algorithm: lan.IdentityKeyAlgorithm, PublicKey: idA.PublicKey}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertVirtualDeviceKey(ctx, store.VirtualDeviceKey{DeviceID: "dev-b", Algorithm: lan.IdentityKeyAlgorithm, PublicKey: idB.PublicKey}); err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ts := time.Now().Unix()
	payloadA := "x25519-a"
	payloadB := "x25519-b"
	a.handleLANRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 15001}, &protocol.Message{
		Type: protocol.MsgLANRegister, From: "dev-a", Peer: "dev-b", Profile: store.ProfileLANPacket,
		Payload: payloadA, Timestamp: ts, Signature: mustSignRegisterPayload(t, idA, "dev-a", "dev-b", store.ProfileLANPacket, ts, payloadA),
	})
	a.handleLANRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 15002}, &protocol.Message{
		Type: protocol.MsgLANRegister, From: "dev-b", Peer: "dev-a", Profile: store.ProfileLANPacket,
		Payload: payloadB, Timestamp: ts, Signature: mustSignRegisterPayload(t, idB, "dev-b", "dev-a", store.ProfileLANPacket, ts, payloadB),
	})
	if _, ok := a.lanPeers["dev-a"][peerSlotKey("dev-b", store.ProfileLANPacket)]; !ok {
		t.Fatalf("missing dev-a LAN peer: %+v", a.lanPeers)
	}
	if _, ok := a.lanPeers["dev-b"][peerSlotKey("dev-a", store.ProfileLANPacket)]; !ok {
		t.Fatalf("missing dev-b LAN peer: %+v", a.lanPeers)
	}
	if _, ok := a.pairs.Load("127.0.0.1:15001"); !ok {
		t.Fatalf("missing LAN relay route for dev-a")
	}
	if _, ok := a.pairs.Load("127.0.0.1:15002"); !ok {
		t.Fatalf("missing LAN relay route for dev-b")
	}
}

func TestLANRegisterDeletesOldRouteOnSocketChange(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	idA, _ := lan.GenerateIdentity()
	if err := a.db.UpsertVirtualDeviceKey(ctx, store.VirtualDeviceKey{DeviceID: "dev-a", Algorithm: lan.IdentityKeyAlgorithm, PublicKey: idA.PublicKey}); err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ts := time.Now().Unix()
	payload := "x25519-a"
	first := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 16001}
	second := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 16002}
	msg := &protocol.Message{
		Type: protocol.MsgLANRegister, From: "dev-a", Peer: "dev-b", Profile: store.ProfileLANPacket,
		Payload: payload, Timestamp: ts, Signature: mustSignRegisterPayload(t, idA, "dev-a", "dev-b", store.ProfileLANPacket, ts, payload),
	}
	a.handleLANRegister(conn, first, msg)
	a.pairs.Store(first.String(), pairRoute{dst: second, lastSeen: time.Now(), sessionID: 1})
	a.handleLANRegister(conn, second, msg)
	if _, ok := a.pairs.Load(first.String()); ok {
		t.Fatal("old LAN pair route should be deleted after socket change")
	}
	if got := a.lanPeers["dev-a"][peerSlotKey("dev-b", store.ProfileLANPacket)].addr.String(); got != second.String() {
		t.Fatalf("new LAN peer address not stored, got %s", got)
	}
}

func TestCleanupExpiredLANPeers(t *testing.T) {
	a := newTestApp(t)
	now := time.Now()
	a.cfg.PeerTTL = time.Second
	a.mu.Lock()
	a.lanPeers["dev-a"] = map[string]*peer{
		peerSlotKey("dev-b", store.ProfileLANPacket): {
			id: "dev-a", want: "dev-b", profile: store.ProfileLANPacket,
			addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 17001}, lastSeen: now.Add(-2 * time.Second),
		},
	}
	a.pairs.Store("127.0.0.1:17001", pairRoute{dst: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 17002}, lastSeen: now, sessionID: 1})
	a.cleanupExpiredPeers(now)
	a.mu.Unlock()
	if len(a.lanPeers) != 0 {
		t.Fatalf("expired LAN peers not cleaned: %+v", a.lanPeers)
	}
	if _, ok := a.pairs.Load("127.0.0.1:17001"); ok {
		t.Fatal("expired LAN peer route not deleted")
	}
}

func TestLANPeerInfoIsPlainJSONWhenServerPSKConfigured(t *testing.T) {
	a := newTestApp(t)
	codec, err := secure.NewCodec(a.cfg.PSK)
	if err != nil {
		t.Fatal(err)
	}
	a.codec = codec
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	dst := client.LocalAddr().(*net.UDPAddr)
	a.sendLANPeer(conn, &peer{id: "dev-a", addr: dst, profile: store.ProfileLANPacket}, &peer{id: "dev-b", addr: dst, lanKey: "x25519-b"})
	buf := make([]byte, 1024)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := client.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	var msg protocol.Message
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		t.Fatalf("LAN peer info must be plain JSON, got %x err=%v", buf[:n], err)
	}
	if msg.Type != protocol.MsgPeerInfo || msg.Peer != "dev-b" || msg.Payload != "x25519-b" {
		t.Fatalf("bad peer info: %+v", msg)
	}
}

func TestLANPlainSTUNResponse(t *testing.T) {
	a := newTestApp(t)
	codec, err := secure.NewCodec(a.cfg.PSK)
	if err != nil {
		t.Fatal(err)
	}
	a.codec = codec
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	req, _ := protocol.Encode(&protocol.Message{Type: protocol.MsgStunReq})
	if _, err := client.WriteToUDP(req, conn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	n, src, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), buf[:n]...)
	if _, _, ok := a.openPacket(data); ok {
		t.Fatal("plain LAN STUN must not require encrypted control frame")
	}
	msg, err := protocol.Decode(data)
	if err != nil || msg.Type != protocol.MsgStunReq {
		t.Fatalf("bad plain stun request: msg=%+v err=%v", msg, err)
	}
	a.writePlainControl(conn, src, &protocol.Message{Type: protocol.MsgStunResp, Addr: src.String()})
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err = client.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	msg, err = protocol.Decode(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != protocol.MsgStunResp || msg.Addr != client.LocalAddr().String() {
		t.Fatalf("bad plain stun response: %+v", msg)
	}
}

func TestHandleDeviceDeleteBlockedByEnabledRule(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	if _, err := a.db.CreateRule(ctx, store.ForwardRule{
		Name: "rdp", SourceID: "dev-a", TargetID: "dev-b", LocalPort: 11388,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodDelete, "/api/admin/devices/dev-a", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "device_in_use" {
		t.Fatalf("expected device_in_use got %v body=%s", resp["code"], rec.Body.String())
	}
}

func TestHandleDevicePatchDisablesAndListShowsHealth(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	if _, err := a.db.CreateRule(ctx, store.ForwardRule{
		Name: "rdp", SourceID: "dev-a", TargetID: "dev-b", LocalPort: 11388,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutTunnelState(ctx, store.TunnelState{
		DeviceID: "dev-a", PeerID: "dev-b", State: "p2p", Via: "p2p", LastError: "",
	}); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodPatch, "/api/admin/devices/dev-b", map[string]any{"enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doAdminJSON(t, a, http.MethodGet, "/api/admin/devices", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var devices []store.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("unexpected devices: %+v", devices)
	}
	var got store.Device
	for _, d := range devices {
		if d.ID == "dev-b" {
			got = d
			break
		}
	}
	if got.Enabled {
		t.Fatalf("expected device disabled: %+v", got)
	}
	if got.HealthSummary != "至少一条隧道正常" {
		t.Fatalf("expected health summary from paired tunnel, got %+v", got)
	}
}

func TestHandleAgentEndpointsRejectDisabledDevice(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", false)

	rec := doAgentJSON(t, a.httpMux(), http.MethodPost, "/api/agent/register", map[string]any{
		"device_id": "dev-a",
		"name":      "dev-a",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "device_disabled" {
		t.Fatalf("expected device_disabled got %v body=%s", resp["code"], rec.Body.String())
	}

	rec = doAgentJSON(t, a.httpMux(), http.MethodGet, "/api/agent/rules?device_id=dev-a", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	resp = map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "device_disabled" {
		t.Fatalf("expected device_disabled got %v body=%s", resp["code"], rec.Body.String())
	}
}

func TestHandleAgentTunnelStatusDoesNotOverwriteNameWithID(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.db.UpsertDevice(ctx, "dev-a", "office-pc", "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetDeviceEnabled(ctx, "dev-a", true); err != nil {
		t.Fatal(err)
	}
	mustUpsertDevice(t, a, ctx, "dev-b", true)

	rec := doAgentJSON(t, a.httpMux(), http.MethodPost, "/api/agent/tunnel-status", map[string]any{
		"device_id": "dev-a",
		"peer":      "dev-b",
		"profile":   "interactive",
		"state":     "p2p",
		"via":       "p2p",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err := a.db.GetDevice(ctx, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "office-pc" {
		t.Fatalf("tunnel status should not overwrite display name with id: %+v", d)
	}
}

func TestAgentRegisterAndHeartbeatEmptyNamePreserveDisplayName(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.db.UpsertDevice(ctx, "dev-a", "office-pc", "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetDeviceEnabled(ctx, "dev-a", true); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/agent/register", "/api/agent/heartbeat"} {
		rec := doAgentJSON(t, a.httpMux(), http.MethodPost, path, map[string]any{
			"device_id": "dev-a",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	d, err := a.db.GetDevice(ctx, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "office-pc" {
		t.Fatalf("empty agent names should preserve display name: %+v", d)
	}
}

func TestHandleClientRelease(t *testing.T) {
	a := newTestApp(t)
	a.cfg.ClientReleaseVersion = "0.4.0"
	a.cfg.ClientReleaseSHA256 = "abc123"
	a.cfg.ClientReleasePublishedAt = "2026-04-28T18:00:00Z"
	a.cfg.ClientReleaseNotes = "stable release"
	a.cfg.ClientReleaseMinimumSupported = "0.3.0"
	a.cfg.ClientReleaseURL = "https://example.com/udp-tunnel-client-0.4.0-setup.exe"

	rec := doAgentJSON(t, a.httpMux(), http.MethodGet, "/api/client/release", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp clientReleaseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Version != "0.4.0" || resp.URL == "" || resp.SHA256 != "abc123" {
		t.Fatalf("unexpected release response: %+v", resp)
	}
}

func TestHandleLANRelease(t *testing.T) {
	a := newTestApp(t)
	a.cfg.LANReleaseVersion = "0.8.0"
	a.cfg.LANReleaseSHA256 = "def456"
	a.cfg.LANReleasePublishedAt = "2026-05-24T10:00:00Z"
	a.cfg.LANReleaseNotes = "lan stable release"
	a.cfg.LANReleaseMinimumSupported = "0.7.0"
	a.cfg.LANReleaseURL = "https://example.com/UDPTunnelLAN-0.8.0-setup.exe"

	rec := doJSON(t, a.httpMux(), http.MethodGet, "/api/lan/release", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp clientReleaseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Version != "0.8.0" || resp.URL == "" || resp.SHA256 != "def456" || resp.MinimumSupportedVersion != "0.7.0" {
		t.Fatalf("unexpected LAN release response: %+v", resp)
	}
}

func TestHandleAdminReleaseUploadPersistsSHA256(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "client.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "installer"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := newJSONRequest(t, http.MethodPost, "/api/admin/releases/client/upload", nil)
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminToken(t, a))
	rec := httptest.NewRecorder()
	a.httpMux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp releaseUploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.SHA256 != "9c0d294c05fc1d88d698034609bb81c0c69196327594e4c69d2915c80fd9850c" {
		t.Fatalf("unexpected sha: %+v", resp)
	}
	if _, err := os.Stat(filepath.Clean(resp.File)); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}
	if got, err := a.db.GetSystemSetting(context.Background(), settingClientReleaseSHA256); err != nil || got != resp.SHA256 {
		t.Fatalf("sha setting not persisted, got=%q err=%v", got, err)
	}
}

func TestHandleAdminReleaseValidateURL(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/releases/validate-url", map[string]any{"url": srv.URL})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doAdminJSON(t, a, http.MethodPost, "/api/admin/releases/validate-url", map[string]any{"url": "file:///tmp/x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApplyStoredSettingsUsesSystemSettings(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.db.PutSystemSetting(ctx, settingPeerTTL, "45s"); err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutSystemSetting(ctx, settingAllowRelay, "false"); err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutSystemSetting(ctx, settingLANAllowRelay, "true"); err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutSystemSetting(ctx, settingClientLogLevel, "debug"); err != nil {
		t.Fatal(err)
	}

	if err := a.applyStoredSettings(); err != nil {
		t.Fatal(err)
	}

	if a.cfg.PeerTTL != 45*time.Second || a.cfg.AllowRelay || !a.cfg.LANAllowRelay || a.cfg.ClientLogLevel != "debug" {
		t.Fatalf("settings not applied from database: %+v", a.cfg)
	}
	if got, err := a.db.GetSystemSetting(ctx, settingPairTTL); err != nil || got != config.DefaultServer().PairTTL.String() {
		t.Fatalf("default setting not persisted: got=%q err=%v", got, err)
	}
}

func TestApplyStoredSettingsMigratesLegacyMeta(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.db.PutMeta(ctx, "setting_peer_ttl", "55s"); err != nil {
		t.Fatal(err)
	}

	if err := a.applyStoredSettings(); err != nil {
		t.Fatal(err)
	}

	if a.cfg.PeerTTL != 55*time.Second {
		t.Fatalf("legacy meta setting was not applied: %+v", a.cfg)
	}
	got, err := a.db.GetSystemSetting(ctx, settingPeerTTL)
	if err != nil {
		t.Fatal(err)
	}
	if got != "55s" {
		t.Fatalf("legacy setting was not migrated, got %q", got)
	}
}

func TestHandleSettingsPersistsSystemSettings(t *testing.T) {
	a := newTestApp(t)
	rec := doAdminJSON(t, a, http.MethodPatch, "/api/admin/settings", map[string]any{
		"peer_ttl":                                 "45s",
		"pair_ttl":                                 "1m",
		"relay_idle_timeout":                       "2m",
		"allow_relay":                              false,
		"lan_allow_relay":                          true,
		"allow_legacy":                             false,
		"client_no_upnp":                           true,
		"client_upnp_timeout":                      "2s",
		"client_log_level":                         "debug",
		"client_tray_enabled":                      false,
		"client_punch_timeout":                     "12s",
		"client_force_relay":                       true,
		"client_allow_legacy":                      false,
		"client_release_version":                   "1.2.3",
		"client_release_url":                       "https://example.com/client.exe",
		"client_release_sha256":                    "abc123",
		"client_release_published_at":              "2026-05-22T12:00:00Z",
		"client_release_notes":                     "release notes",
		"client_release_minimum_supported_version": "1.0.0",
		"client_release_file":                      "client.exe",
		"lan_release_version":                      "0.8.0",
		"lan_release_url":                          "https://example.com/lan.exe",
		"lan_release_sha256":                       "def456",
		"lan_release_published_at":                 "2026-05-24T10:00:00Z",
		"lan_release_notes":                        "lan release notes",
		"lan_release_minimum_supported_version":    "0.7.0",
		"lan_release_file":                         "lan.exe",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got, err := a.db.GetSystemSetting(context.Background(), settingClientLogLevel)
	if err != nil {
		t.Fatal(err)
	}
	if got != "debug" {
		t.Fatalf("expected setting in system settings table, got %q", got)
	}
	lanRelay, err := a.db.GetSystemSetting(context.Background(), settingLANAllowRelay)
	if err != nil {
		t.Fatal(err)
	}
	if lanRelay != "true" || !a.cfg.LANAllowRelay {
		t.Fatalf("expected LAN relay setting persisted, got=%q cfg=%v", lanRelay, a.cfg.LANAllowRelay)
	}
	lanRelease, err := a.db.GetSystemSetting(context.Background(), settingLANReleaseVersion)
	if err != nil {
		t.Fatal(err)
	}
	if lanRelease != "0.8.0" || a.cfg.LANReleaseURL != "https://example.com/lan.exe" {
		t.Fatalf("expected LAN release settings persisted, version=%q cfg=%+v", lanRelease, a.cfg)
	}
	legacy, err := a.db.GetMeta(context.Background(), "setting_client_log_level")
	if err != nil {
		t.Fatal(err)
	}
	if legacy != "" {
		t.Fatalf("settings should not be persisted to meta, got %q", legacy)
	}
}

func TestHandleAuditEventsFilters(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.db.Audit(ctx, "rule_create", "#1 dev-a->dev-b:3389"); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Audit(ctx, "device_delete", "dev-old"); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodGet, "/api/admin/audit-events?kind=rule_create&keyword=3389", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var events []store.AuditEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "rule_create" || events[0].Detail != "#1 dev-a->dev-b:3389" {
		t.Fatalf("unexpected audit events: %+v", events)
	}
}

func TestAdminJWTLoginRefreshAndMe(t *testing.T) {
	a := newTestApp(t)
	pass := "secret-pass"
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertAdminUser(context.Background(), store.AdminUser{
		ID: "admin", Username: "admin", Name: "Administrator", Role: "admin", PasswordHash: string(hash),
	}); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{"username": "admin", "password": pass}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	var loginResp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatal(err)
	}
	if loginResp.AccessToken == "" || loginResp.RefreshToken == "" {
		t.Fatalf("missing tokens: %+v", loginResp)
	}

	rec = doJSON(t, a.httpMux(), http.MethodGet, "/api/admin/me", nil, map[string]string{"Authorization": "Bearer " + loginResp.AccessToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/refresh", map[string]any{"refresh_token": loginResp.RefreshToken}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", rec.Code, rec.Body.String())
	}
	var refreshResp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &refreshResp); err != nil {
		t.Fatal(err)
	}
	if refreshResp.AccessToken == "" || refreshResp.RefreshToken == "" || refreshResp.RefreshToken == loginResp.RefreshToken {
		t.Fatalf("bad refresh response: %+v", refreshResp)
	}
}

func TestEnsureAdminPasswordCreatesDefaultAdmin(t *testing.T) {
	a := newTestApp(t)
	if err := a.ensureAdminUser(); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{"username": defaultAdminUsername, "password": defaultAdminPassword}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	var loginResp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatal(err)
	}
	if !loginResp.ForcePasswordChange {
		t.Fatalf("expected default admin to require password change: %+v", loginResp)
	}
}

func TestEnsureAdminUserMigratesLegacyPasswordHash(t *testing.T) {
	a := newTestApp(t)
	pass := "legacy-pass"
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutMeta(context.Background(), "admin_password_hash", string(hash)); err != nil {
		t.Fatal(err)
	}
	if err := a.ensureAdminUser(); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{"username": defaultAdminUsername, "password": pass}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChangePasswordClearsForceFlag(t *testing.T) {
	a := newTestApp(t)
	pass := "secret-pass"
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertAdminUser(context.Background(), store.AdminUser{
		ID: "admin", Username: "admin", Name: "Administrator", Role: "admin", ForcePasswordChange: true, PasswordVersion: 1, PasswordHash: string(hash),
	}); err != nil {
		t.Fatal(err)
	}
	loginRec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{"username": "admin", "password": pass}, nil)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp tokenResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/password", map[string]any{
		"current_password": pass,
		"new_password":     "new-secret-pass",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password status=%d body=%s", rec.Code, rec.Body.String())
	}
	user, err := a.db.GetAdminUserByID(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if user.ForcePasswordChange {
		t.Fatalf("force flag not cleared: %+v", user)
	}

	rec = doJSON(t, a.httpMux(), http.MethodGet, "/api/admin/me", nil, map[string]string{"Authorization": "Bearer " + loginResp.AccessToken})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old access token should be invalidated after password change, got=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChangePasswordRevokesExistingRefreshTokens(t *testing.T) {
	a := newTestApp(t)
	pass := "secret-pass"
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertAdminUser(context.Background(), store.AdminUser{
		ID: "admin", Username: "admin", Name: "Administrator", Role: "admin", ForcePasswordChange: true, PasswordVersion: 1, PasswordHash: string(hash),
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.CreateAdminRefreshToken(context.Background(), "admin", "old-refresh", time.Now().Add(time.Hour), "ua", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/password", map[string]any{
		"current_password": pass,
		"new_password":     "new-secret-pass",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password status=%d body=%s", rec.Code, rec.Body.String())
	}
	token, err := a.db.GetAdminRefreshToken(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if token.RevokedAt == "" {
		t.Fatalf("refresh token not revoked after password change: %+v", token)
	}
}

func TestAdminConsoleAPISmoke(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)

	rec := doAdminJSON(t, a, http.MethodGet, "/api/admin/devices", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("devices status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doAdminJSON(t, a, http.MethodPost, "/api/admin/rules", map[string]any{
		"name": "rdp", "source_id": "dev-a", "target_id": "dev-b", "profile": "interactive",
		"local_port": 13389, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create rule status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rule store.ForwardRule
	if err := json.Unmarshal(rec.Body.Bytes(), &rule); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/admin/rules", nil},
		{http.MethodGet, "/api/admin/sessions", nil},
		{http.MethodGet, "/api/admin/tunnel-states", nil},
		{http.MethodGet, "/api/admin/metrics", nil},
		{http.MethodGet, "/api/admin/settings", nil},
		{http.MethodPatch, "/api/admin/rules/" + strconv.FormatInt(rule.ID, 10), map[string]any{
			"name": "rdp", "source_id": "dev-a", "target_id": "dev-b", "profile": "interactive",
			"local_port": 13390, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true,
		}},
		{http.MethodPatch, "/api/admin/devices/dev-b", map[string]any{"enabled": false}},
	}
	for _, check := range checks {
		rec = doAdminJSON(t, a, check.method, check.path, check.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", check.method, check.path, rec.Code, rec.Body.String())
		}
	}
}

func TestEndToEndAdminAgentBootstrapFlow(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.ensureAdminUser(); err != nil {
		t.Fatal(err)
	}

	loginRec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{
		"username": defaultAdminUsername,
		"password": defaultAdminPassword,
	}, nil)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp tokenResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatal(err)
	}
	if !loginResp.ForcePasswordChange {
		t.Fatalf("default admin should require password change: %+v", loginResp)
	}

	changeRec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/password", map[string]any{
		"current_password": defaultAdminPassword,
		"new_password":     "admin-secret-123",
	}, map[string]string{"Authorization": "Bearer " + loginResp.AccessToken})
	if changeRec.Code != http.StatusOK {
		t.Fatalf("change password status=%d body=%s", changeRec.Code, changeRec.Body.String())
	}

	oldMeRec := doJSON(t, a.httpMux(), http.MethodGet, "/api/admin/me", nil, map[string]string{"Authorization": "Bearer " + loginResp.AccessToken})
	if oldMeRec.Code != http.StatusUnauthorized {
		t.Fatalf("old access token should be invalid after password change, got=%d body=%s", oldMeRec.Code, oldMeRec.Body.String())
	}

	loginRec = doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{
		"username": defaultAdminUsername,
		"password": "admin-secret-123",
	}, nil)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("second login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatal(err)
	}
	adminHeaders := map[string]string{"Authorization": "Bearer " + loginResp.AccessToken}

	settingsRec := doJSON(t, a.httpMux(), http.MethodPatch, "/api/admin/settings", map[string]any{
		"peer_ttl":                                 "45s",
		"pair_ttl":                                 "1m",
		"relay_idle_timeout":                       "2m",
		"allow_relay":                              true,
		"lan_allow_relay":                          true,
		"allow_legacy":                             false,
		"client_no_upnp":                           true,
		"client_upnp_timeout":                      "3s",
		"client_log_level":                         "debug",
		"client_tray_enabled":                      false,
		"client_punch_timeout":                     "15s",
		"client_force_relay":                       true,
		"client_allow_legacy":                      false,
		"client_release_version":                   "1.0.0",
		"client_release_url":                       "https://example.com/client.exe",
		"client_release_sha256":                    "abc",
		"client_release_published_at":              "2026-05-23T10:00:00+08:00",
		"client_release_notes":                     "stable",
		"client_release_minimum_supported_version": "0.9.0",
		"client_release_file":                      "",
		"lan_release_version":                      "0.8.0",
		"lan_release_url":                          "https://example.com/lan.exe",
		"lan_release_sha256":                       "def",
		"lan_release_published_at":                 "2026-05-23T11:00:00+08:00",
		"lan_release_notes":                        "lan stable",
		"lan_release_minimum_supported_version":    "0.7.0",
		"lan_release_file":                         "",
	}, adminHeaders)
	if settingsRec.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", settingsRec.Code, settingsRec.Body.String())
	}

	bootstrapRec := doAgentJSON(t, a.httpMux(), http.MethodPost, "/api/agent/bootstrap", map[string]any{
		"device_id":   "dev-a",
		"device_name": "Office A",
	})
	if bootstrapRec.Code != http.StatusOK {
		t.Fatalf("bootstrap dev-a status=%d body=%s", bootstrapRec.Code, bootstrapRec.Body.String())
	}
	var bootstrap agentBootstrapResponse
	if err := json.Unmarshal(bootstrapRec.Body.Bytes(), &bootstrap); err != nil {
		t.Fatal(err)
	}
	if !bootstrap.NoUPnP || bootstrap.UPnPTimeout != "3s" || bootstrap.LogLevel != "debug" || !bootstrap.ForceRelay {
		t.Fatalf("bootstrap did not use stored settings: %+v", bootstrap)
	}
	if bootstrap.PSK != a.cfg.PSK {
		t.Fatalf("bootstrap should include server psk: %+v", bootstrap)
	}

	bootstrapRec = doAgentJSON(t, a.httpMux(), http.MethodPost, "/api/agent/bootstrap", map[string]any{
		"device_id":   "dev-b",
		"device_name": "Office B",
	})
	if bootstrapRec.Code != http.StatusOK {
		t.Fatalf("bootstrap dev-b status=%d body=%s", bootstrapRec.Code, bootstrapRec.Body.String())
	}
	if err := a.db.SetDeviceEnabled(ctx, "dev-a", true); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetDeviceEnabled(ctx, "dev-b", true); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"dev-a", "dev-b"} {
		rec := doAgentJSON(t, a.httpMux(), http.MethodPost, "/api/agent/register", map[string]any{
			"device_id": id,
			"name":      id,
			"tunnels": []map[string]any{{
				"peer":    otherDevice(id),
				"profile": "interactive",
				"state":   "connecting",
			}},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("register %s status=%d body=%s", id, rec.Code, rec.Body.String())
		}
	}

	ruleRec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/rules", map[string]any{
		"name": "rdp", "source_id": "dev-a", "target_id": "dev-b", "profile": "interactive",
		"local_port": 13389, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true,
	}, adminHeaders)
	if ruleRec.Code != http.StatusOK {
		t.Fatalf("create rule status=%d body=%s", ruleRec.Code, ruleRec.Body.String())
	}

	rulesRec := doAgentJSON(t, a.httpMux(), http.MethodGet, "/api/agent/rules?device_id=dev-a", nil)
	if rulesRec.Code != http.StatusOK {
		t.Fatalf("agent rules status=%d body=%s", rulesRec.Code, rulesRec.Body.String())
	}
	var rules []store.ForwardRule
	if err := json.Unmarshal(rulesRec.Body.Bytes(), &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].TargetID != "dev-b" {
		t.Fatalf("unexpected agent rules: %+v", rules)
	}

	sessionID, err := a.db.StartSession(ctx, "dev-a", "dev-b", store.ProfileInteractive, "pending")
	if err != nil {
		t.Fatal(err)
	}
	statusRec := doAgentJSON(t, a.httpMux(), http.MethodPost, "/api/agent/tunnel-status", map[string]any{
		"device_id": "dev-a",
		"peer":      "dev-b",
		"profile":   "interactive",
		"state":     "relay",
		"via":       "relay",
		"rtt_ms":    12,
	})
	if statusRec.Code != http.StatusOK {
		t.Fatalf("tunnel status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}
	sessionsRec := doJSON(t, a.httpMux(), http.MethodGet, "/api/admin/sessions", nil, adminHeaders)
	if sessionsRec.Code != http.StatusOK {
		t.Fatalf("sessions status=%d body=%s", sessionsRec.Code, sessionsRec.Body.String())
	}
	var sessions []store.Session
	if err := json.Unmarshal(sessionsRec.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) == 0 || sessions[0].ID != sessionID || sessions[0].Path != "relay" {
		t.Fatalf("session path not updated: %+v", sessions)
	}
}

func TestErrorResponsesUseJSONContract(t *testing.T) {
	a := newTestApp(t)
	checks := []struct {
		name   string
		rec    *httptest.ResponseRecorder
		status int
		code   string
	}{
		{
			name:   "agent unauthorized",
			rec:    doJSON(t, a.httpMux(), http.MethodPost, "/api/agent/register", map[string]any{"device_id": "dev-a"}, nil),
			status: http.StatusUnauthorized,
			code:   "unauthorized",
		},
		{
			name:   "admin login unauthorized",
			rec:    doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{"username": "admin", "password": "wrong"}, nil),
			status: http.StatusUnauthorized,
			code:   "unauthorized",
		},
		{
			name:   "admin refresh bad json",
			rec:    doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/refresh", map[string]any{}, nil),
			status: http.StatusBadRequest,
			code:   "bad_json",
		},
		{
			name:   "admin me unauthorized",
			rec:    doJSON(t, a.httpMux(), http.MethodGet, "/api/admin/me", nil, nil),
			status: http.StatusUnauthorized,
			code:   "unauthorized",
		},
		{
			name:   "bad settings duration",
			rec:    doAdminJSON(t, a, http.MethodPatch, "/api/admin/settings", map[string]any{"peer_ttl": "bad"}),
			status: http.StatusBadRequest,
			code:   "bad_peer_ttl",
		},
		{
			name:   "bad agent request",
			rec:    doAgentJSON(t, a.httpMux(), http.MethodPost, "/api/agent/register", map[string]any{}),
			status: http.StatusBadRequest,
			code:   "bad_json",
		},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if check.rec.Code != check.status {
				t.Fatalf("status=%d body=%s", check.rec.Code, check.rec.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(check.rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("response is not json: %v body=%s", err, check.rec.Body.String())
			}
			if resp["code"] != check.code || resp["error"] == "" {
				t.Fatalf("unexpected error contract: %+v", resp)
			}
		})
	}
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	db := newFakeStore()
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultServer()
	cfg.PSK = "test-psk"
	return &App{
		cfg:       cfg,
		db:        db,
		startTime: time.Now(),
		peers:     map[string]map[string]*peer{},
		pairByID:  map[string]int64{},
		lanRelay:  newLANPacketRelay(256),
		lanPeers:  map[string]map[string]*peer{},
	}
}

func mustUpsertDevice(t *testing.T, a *App, ctx context.Context, id string, enabled bool) {
	t.Helper()
	if err := a.db.UpsertDevice(ctx, id, id, "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetDeviceEnabled(ctx, id, enabled); err != nil {
		t.Fatal(err)
	}
}

func otherDevice(id string) string {
	if id == "dev-a" {
		return "dev-b"
	}
	return "dev-a"
}

func mustSignRegisterPayload(t *testing.T, id lan.Identity, from, peer, profile string, ts int64, payload string) string {
	t.Helper()
	sig, err := lan.SignRegisterPayload(id, from, peer, profile, ts, payload)
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func doAdminJSON(t *testing.T, a *App, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	token := adminToken(t, a)
	req := newJSONRequest(t, method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.httpMux().ServeHTTP(rec, req)
	return rec
}

func adminToken(t *testing.T, a *App) string {
	t.Helper()
	if _, err := a.db.GetAdminUserByID(context.Background(), defaultAdminUsername); err != nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.db.UpsertAdminUser(context.Background(), store.AdminUser{
			ID: defaultAdminUsername, Username: defaultAdminUsername, Name: "Administrator", Role: "admin", PasswordVersion: 1, PasswordHash: string(hash),
		}); err != nil {
			t.Fatal(err)
		}
	}
	token, err := a.signAccessToken(adminClaims{
		Subject:         defaultAdminUsername,
		Role:            "admin",
		PasswordVersion: 1,
		Issued:          time.Now().Unix(),
		Expires:         time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func doAgentJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := newJSONRequest(t, method, path, body)
	req.Header.Set("X-UDP-Tunnel-PSK", "test-psk")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := newJSONRequest(t, method, path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newJSONRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	return req
}
