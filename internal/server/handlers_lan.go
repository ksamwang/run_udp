package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"udp_tunnel_demo/internal/store"
	"udp_tunnel_demo/internal/vnet"
)

const lanBootstrapVersion = 1

type lanBootstrapResponse struct {
	Version       int                    `json:"version"`
	Capabilities  []string               `json:"capabilities"`
	ConfigVersion string                 `json:"config_version"`
	Server        string                 `json:"server"`
	STUNAltPort   int                    `json:"stun_alt_port"`
	DeviceID      string                 `json:"device_id"`
	DeviceName    string                 `json:"device_name"`
	Network       store.VirtualNetwork   `json:"network"`
	Address       store.VirtualAddress   `json:"address"`
	Routes        []store.VirtualRoute   `json:"routes"`
	ACL           []store.VirtualACLRule `json:"acl"`
	Peers         []lanBootstrapPeer     `json:"peers"`
}

type lanBootstrapPeer struct {
	DeviceID  string `json:"device_id"`
	VirtualIP string `json:"virtual_ip"`
	Hostname  string `json:"hostname"`
	PublicKey string `json:"public_key"`
}

type lanDeviceState struct {
	DeviceID        string `json:"device_id"`
	NetworkID       int64  `json:"network_id"`
	VirtualIP       string `json:"virtual_ip"`
	Hostname        string `json:"hostname"`
	AdapterState    string `json:"adapter_state"`
	SelectedCIDR    string `json:"selected_cidr"`
	RouteConflict   string `json:"route_conflict"`
	P2PPeers        int    `json:"p2p_peers"`
	RelayPeers      int    `json:"relay_peers"`
	DownPeers       int    `json:"down_peers"`
	LastBootstrapAt string `json:"last_bootstrap_at"`
	LastStatusAt    string `json:"last_status_at"`
	LastError       string `json:"last_error"`
}

type lanDiagnosticsSnapshot struct {
	GeneratedAt  string                       `json:"generated_at"`
	NetworkID    int64                        `json:"network_id,omitempty"`
	Networks     []store.VirtualNetwork       `json:"networks"`
	Addresses    []store.VirtualAddress       `json:"addresses"`
	DeviceKeys   []store.VirtualDeviceKey     `json:"device_keys"`
	Groups       []store.VirtualDeviceGroup   `json:"groups"`
	ACL          []store.VirtualACLRule       `json:"acl"`
	Routes       []store.VirtualRoute         `json:"routes"`
	PeerStates   []store.VirtualPeerState     `json:"peer_states"`
	DeviceStates []lanDeviceState             `json:"device_states"`
	PathEvents   []store.VirtualPeerPathEvent `json:"path_events"`
}

func (a *App) handleAdminLANNetworks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		networks, err := a.db.ListVirtualNetworks(r.Context())
		writeJSONOrError(w, networks, err)
	case http.MethodPost:
		network, err := decodeVirtualNetwork(r)
		if err == nil {
			network, err = a.db.CreateVirtualNetwork(r.Context(), network)
		}
		writeJSONOrError(w, network, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminLANNetwork(w http.ResponseWriter, r *http.Request) {
	id, err := parseLANPathID(r.URL.Path, "/api/admin/lan/networks/")
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		network, err := decodeVirtualNetwork(r)
		if err == nil {
			err = a.db.UpdateVirtualNetwork(r.Context(), id, network)
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	case http.MethodDelete:
		err := a.ensureVirtualNetworkEmpty(r.Context(), id)
		if err == nil {
			err = a.db.DeleteVirtualNetwork(r.Context(), id)
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) ensureVirtualNetworkEmpty(ctx context.Context, networkID int64) error {
	addresses, err := a.db.ListVirtualAddresses(ctx, networkID)
	if err != nil {
		return err
	}
	if len(addresses) > 0 {
		return badRequest("network_in_use", "network still has virtual addresses")
	}
	acl, err := a.db.ListVirtualACLRules(ctx, networkID)
	if err != nil {
		return err
	}
	if len(acl) > 0 {
		return badRequest("network_in_use", "network still has acl rules")
	}
	routes, err := a.db.ListVirtualRoutes(ctx, networkID, "")
	if err != nil {
		return err
	}
	if len(routes) > 0 {
		return badRequest("network_in_use", "network still has virtual routes")
	}
	states, err := a.db.ListVirtualPeerStates(ctx, networkID)
	if err != nil {
		return err
	}
	if len(states) > 0 {
		return badRequest("network_in_use", "network still has peer states")
	}
	return nil
}

func (a *App) handleAdminLANAddresses(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		networkID, err := optionalInt64Query(r, "network_id")
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		addresses, err := a.db.ListVirtualAddresses(r.Context(), networkID)
		writeJSONOrError(w, addresses, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminLANAddress(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/lan/addresses/"))
	if deviceID == "" {
		writeJSONOrError(w, nil, badRequest("bad_device_id", "bad device id"))
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var address store.VirtualAddress
		if err := json.NewDecoder(r.Body).Decode(&address); err != nil {
			writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
			return
		}
		address.DeviceID = deviceID
		if err := validateVirtualAddress(address); err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		if err := a.validateVirtualAddressCIDR(r.Context(), address); err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		err := a.db.UpsertVirtualAddress(r.Context(), address)
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	case http.MethodDelete:
		networkID, err := requiredInt64Query(r, "network_id")
		if err == nil {
			err = a.db.DeleteVirtualAddress(r.Context(), networkID, deviceID)
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminLANAddressAction(w http.ResponseWriter, r *http.Request) {
	deviceID, action := parseLANAddressAction(r.URL.Path)
	if deviceID == "" || action == "" {
		writeJSONOrError(w, nil, badRequest("bad_path", "bad address action path"))
		return
	}
	if r.Method != http.MethodPost {
		writeJSONOrError(w, nil, methodNotAllowed())
		return
	}
	networkID, err := requiredInt64Query(r, "network_id")
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	switch action {
	case "release":
		err = a.db.DeleteVirtualAddress(r.Context(), networkID, deviceID)
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	case "reassign":
		err = a.db.DeleteVirtualAddress(r.Context(), networkID, deviceID)
		if err == nil || errors.Is(err, sql.ErrNoRows) {
			var network store.VirtualNetwork
			network, err = a.virtualNetworkByID(r.Context(), networkID)
			if err == nil {
				_, err = a.allocateVirtualAddress(r.Context(), network, deviceID, "")
			}
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	case "bootstrap":
		err = a.touchVirtualAddress(r.Context(), networkID, deviceID)
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		writeJSONOrError(w, nil, badRequest("bad_action", "unsupported address action"))
	}
}

func parseLANAddressAction(path string) (string, string) {
	raw := strings.TrimPrefix(path, "/api/admin/lan/addresses/")
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func (a *App) handleAdminLANDeviceKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys, err := a.db.ListVirtualDeviceKeys(r.Context())
		writeJSONOrError(w, keys, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

type lanGroupResponse struct {
	store.VirtualDeviceGroup
	DeviceIDs []string `json:"device_ids"`
}

func (a *App) handleAdminLANGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		groups, err := a.listLANGroups(r.Context())
		writeJSONOrError(w, groups, err)
	case http.MethodPost:
		group, members, err := decodeLANGroup(r)
		if err == nil {
			group, err = a.db.UpsertVirtualDeviceGroup(r.Context(), group)
		}
		if err == nil {
			err = a.db.SetVirtualDeviceGroupMembers(r.Context(), group.ID, members)
		}
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		writeJSONOrError(w, lanGroupResponse{VirtualDeviceGroup: group, DeviceIDs: members}, nil)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminLANGroup(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/lan/groups/"))
	if id == "" {
		writeJSONOrError(w, nil, badRequest("bad_group_id", "bad group id"))
		return
	}
	switch r.Method {
	case http.MethodPatch:
		group, members, err := decodeLANGroup(r)
		if err == nil {
			group.ID = id
			group, err = a.db.UpsertVirtualDeviceGroup(r.Context(), group)
		}
		if err == nil {
			err = a.db.SetVirtualDeviceGroupMembers(r.Context(), group.ID, members)
		}
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		writeJSONOrError(w, map[string]any{"ok": true}, nil)
	case http.MethodDelete:
		err := a.db.DeleteVirtualDeviceGroup(r.Context(), id)
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) listLANGroups(ctx context.Context) ([]lanGroupResponse, error) {
	groups, err := a.db.ListVirtualDeviceGroups(ctx)
	if err != nil {
		return nil, err
	}
	members, err := a.db.ListVirtualDeviceGroupMembers(ctx, "")
	if err != nil {
		return nil, err
	}
	byGroup := map[string][]string{}
	for _, member := range members {
		byGroup[member.GroupID] = append(byGroup[member.GroupID], member.DeviceID)
	}
	out := make([]lanGroupResponse, 0, len(groups))
	for _, group := range groups {
		out = append(out, lanGroupResponse{VirtualDeviceGroup: group, DeviceIDs: byGroup[group.ID]})
	}
	return out, nil
}

func (a *App) handleAdminLANACLRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		networkID, err := optionalInt64Query(r, "network_id")
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		rules, err := a.db.ListVirtualACLRules(r.Context(), networkID)
		writeJSONOrError(w, rules, err)
	case http.MethodPost:
		rule, err := decodeVirtualACLRule(r)
		if err == nil {
			rule, err = a.db.CreateVirtualACLRule(r.Context(), rule)
		}
		writeJSONOrError(w, rule, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminLANACLRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseLANPathID(r.URL.Path, "/api/admin/lan/acl/")
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		rule, err := decodeVirtualACLRule(r)
		if err == nil {
			err = a.db.UpdateVirtualACLRule(r.Context(), id, rule)
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	case http.MethodDelete:
		err := a.db.DeleteVirtualACLRule(r.Context(), id)
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminLANRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		networkID, err := optionalInt64Query(r, "network_id")
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
		routes, err := a.db.ListVirtualRoutes(r.Context(), networkID, deviceID)
		writeJSONOrError(w, routes, err)
	case http.MethodPost:
		route, err := a.decodeVirtualRoute(r)
		if err == nil {
			err = a.db.UpsertVirtualRoute(r.Context(), route)
		}
		if err == nil {
			routes, listErr := a.db.ListVirtualRoutes(r.Context(), route.NetworkID, route.DeviceID)
			if listErr != nil {
				err = listErr
			} else {
				for _, item := range routes {
					if item.CIDR == route.CIDR {
						route = item
						break
					}
				}
			}
		}
		writeJSONOrError(w, route, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminLANRoute(w http.ResponseWriter, r *http.Request) {
	id, err := parseLANPathID(r.URL.Path, "/api/admin/lan/routes/")
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		route, err := a.decodeVirtualRoute(r)
		if err == nil {
			route.ID = id
			err = a.db.UpsertVirtualRoute(r.Context(), route)
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	case http.MethodDelete:
		err := a.db.DeleteVirtualRoute(r.Context(), id)
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminLANDeviceStates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		networkID, err := optionalInt64Query(r, "network_id")
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		states, err := a.lanDeviceStates(r.Context(), networkID)
		writeJSONOrError(w, states, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) lanDeviceStates(ctx context.Context, networkID int64) ([]lanDeviceState, error) {
	addresses, err := a.db.ListVirtualAddresses(ctx, networkID)
	if err != nil {
		return nil, err
	}
	peers, err := a.db.ListVirtualPeerStates(ctx, networkID)
	if err != nil {
		return nil, err
	}
	byDevice := map[string]*lanDeviceState{}
	for _, address := range addresses {
		state := byDevice[address.DeviceID]
		if state == nil {
			state = &lanDeviceState{DeviceID: address.DeviceID, NetworkID: address.NetworkID}
			byDevice[address.DeviceID] = state
		}
		state.VirtualIP = address.VirtualIP
		state.Hostname = address.Hostname
		state.LastBootstrapAt = address.UpdatedAt
	}
	for _, peer := range peers {
		state := byDevice[peer.DeviceID]
		if state == nil {
			state = &lanDeviceState{DeviceID: peer.DeviceID, NetworkID: peer.NetworkID}
			byDevice[peer.DeviceID] = state
		}
		if peer.AdapterState != "" {
			state.AdapterState = peer.AdapterState
		}
		if peer.SelectedCIDR != "" {
			state.SelectedCIDR = peer.SelectedCIDR
		}
		if peer.RouteConflict != "" {
			state.RouteConflict = peer.RouteConflict
		}
		if peer.LastError != "" {
			state.LastError = peer.LastError
		}
		if peer.UpdatedAt > state.LastStatusAt {
			state.LastStatusAt = peer.UpdatedAt
		}
		switch strings.ToLower(strings.TrimSpace(peer.Path)) {
		case "p2p":
			state.P2PPeers++
		case "relay":
			state.RelayPeers++
		default:
			state.DownPeers++
		}
	}
	out := make([]lanDeviceState, 0, len(byDevice))
	for _, state := range byDevice {
		out = append(out, *state)
	}
	return out, nil
}

func (a *App) handleAdminLANPeerStates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		networkID, err := optionalInt64Query(r, "network_id")
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		states, err := a.db.ListVirtualPeerStates(r.Context(), networkID)
		writeJSONOrError(w, states, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminLANPathEvents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		networkID, err := optionalInt64Query(r, "network_id")
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		limit, err := optionalIntQuery(r, "limit")
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		events, err := a.db.ListVirtualPeerPathEvents(r.Context(), networkID, r.URL.Query().Get("device_id"), r.URL.Query().Get("peer_id"), limit)
		writeJSONOrError(w, events, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminLANDiagnostics(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		networkID, err := optionalInt64Query(r, "network_id")
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		snapshot, err := a.lanDiagnosticsSnapshot(r.Context(), networkID)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename=\"udptunnellan-diagnostics.json\"")
		writeJSONOrError(w, snapshot, nil)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) lanDiagnosticsSnapshot(ctx context.Context, networkID int64) (lanDiagnosticsSnapshot, error) {
	snapshot := lanDiagnosticsSnapshot{
		GeneratedAt: time.Now().Format(time.RFC3339),
		NetworkID:   networkID,
	}
	var err error
	if snapshot.Networks, err = a.db.ListVirtualNetworks(ctx); err != nil {
		return snapshot, err
	}
	if snapshot.Addresses, err = a.db.ListVirtualAddresses(ctx, networkID); err != nil {
		return snapshot, err
	}
	if snapshot.DeviceKeys, err = a.db.ListVirtualDeviceKeys(ctx); err != nil {
		return snapshot, err
	}
	if snapshot.Groups, err = a.db.ListVirtualDeviceGroups(ctx); err != nil {
		return snapshot, err
	}
	if snapshot.ACL, err = a.db.ListVirtualACLRules(ctx, networkID); err != nil {
		return snapshot, err
	}
	if snapshot.Routes, err = a.db.ListVirtualRoutes(ctx, networkID, ""); err != nil {
		return snapshot, err
	}
	if snapshot.PeerStates, err = a.db.ListVirtualPeerStates(ctx, networkID); err != nil {
		return snapshot, err
	}
	if snapshot.DeviceStates, err = a.lanDeviceStates(ctx, networkID); err != nil {
		return snapshot, err
	}
	if snapshot.PathEvents, err = a.db.ListVirtualPeerPathEvents(ctx, networkID, "", "", 500); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func (a *App) handleLANBootstrap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID     string   `json:"device_id"`
		DeviceName   string   `json:"device_name"`
		PublicKey    string   `json:"public_key"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DeviceID) == "" {
		writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
		return
	}
	network, address, err := a.lanNetworkAndAddress(r.Context(), req.DeviceID)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if err := a.db.UpsertDevice(r.Context(), req.DeviceID, strings.TrimSpace(req.DeviceName), requestAddr(r), "", "", false); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if strings.TrimSpace(req.PublicKey) != "" {
		if err := a.db.UpsertVirtualDeviceKey(r.Context(), store.VirtualDeviceKey{
			DeviceID: req.DeviceID, Algorithm: "ed25519", PublicKey: strings.TrimSpace(req.PublicKey),
		}); err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
	}
	if strings.TrimSpace(address.DeviceID) == "" {
		address, err = a.allocateVirtualAddress(r.Context(), network, req.DeviceID, strings.TrimSpace(req.DeviceName))
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
	} else if strings.TrimSpace(address.VirtualIP) == "" {
		address, err = a.allocateVirtualAddress(r.Context(), network, req.DeviceID, firstNonEmpty(address.Hostname, strings.TrimSpace(req.DeviceName)))
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
	}
	routes, err := a.db.ListVirtualRoutes(r.Context(), network.ID, req.DeviceID)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	acl, err := a.db.ListVirtualACLRules(r.Context(), network.ID)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	addresses, err := a.db.ListVirtualAddresses(r.Context(), network.ID)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	keys, err := a.db.ListVirtualDeviceKeys(r.Context())
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	acl, err = a.expandVirtualACLRules(r.Context(), acl, addresses)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	keyByDevice := make(map[string]string, len(keys))
	for _, key := range keys {
		if key.Algorithm == "ed25519" {
			keyByDevice[key.DeviceID] = key.PublicKey
		}
	}
	peers := make([]lanBootstrapPeer, 0, len(addresses))
	for _, peer := range addresses {
		if peer.DeviceID == req.DeviceID {
			continue
		}
		peers = append(peers, lanBootstrapPeer{
			DeviceID: peer.DeviceID, VirtualIP: peer.VirtualIP, Hostname: peer.Hostname, PublicKey: keyByDevice[peer.DeviceID],
		})
	}
	writeJSON(w, http.StatusOK, lanBootstrapResponse{
		Version: lanBootstrapVersion, Capabilities: []string{"ipv4", "tcp", "rdp"},
		ConfigVersion: lanConfigVersion(network, addresses, acl, routes), Server: externalUDPAddr(r, a.cfg.UDPListen), STUNAltPort: portFromAddr(a.cfg.StunAltListen, 7002), DeviceID: req.DeviceID,
		DeviceName: strings.TrimSpace(req.DeviceName), Network: network, Address: address, Routes: routes, ACL: acl, Peers: peers,
	})
}

func (a *App) lanNetworkAndAddress(ctx context.Context, deviceID string) (store.VirtualNetwork, store.VirtualAddress, error) {
	address, err := a.db.GetVirtualAddressByDevice(ctx, deviceID)
	if err == nil {
		networks, err := a.db.ListVirtualNetworks(ctx)
		if err != nil {
			return store.VirtualNetwork{}, store.VirtualAddress{}, err
		}
		for _, network := range networks {
			if network.ID == address.NetworkID {
				return network, address, nil
			}
		}
		return store.VirtualNetwork{}, store.VirtualAddress{}, sql.ErrNoRows
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return store.VirtualNetwork{}, store.VirtualAddress{}, err
	}
	network, err := a.db.EnsureDefaultVirtualNetwork(ctx)
	return network, store.VirtualAddress{}, err
}

type virtualAddressStore interface {
	ListVirtualAddresses(ctx context.Context, networkID int64) ([]store.VirtualAddress, error)
	UpsertVirtualAddress(ctx context.Context, address store.VirtualAddress) error
}

func (a *App) allocateVirtualAddress(ctx context.Context, network store.VirtualNetwork, deviceID, hostname string) (store.VirtualAddress, error) {
	return allocateVirtualAddress(ctx, a.db, network, deviceID, hostname)
}

func allocateVirtualAddress(ctx context.Context, db virtualAddressStore, network store.VirtualNetwork, deviceID, hostname string) (store.VirtualAddress, error) {
	_, ipNet, err := net.ParseCIDR(network.CIDR)
	if err != nil {
		return store.VirtualAddress{}, err
	}
	base := ipNet.IP.To4()
	if base == nil {
		return store.VirtualAddress{}, badRequest("bad_network", "only ipv4 network is supported")
	}
	addresses, err := db.ListVirtualAddresses(ctx, network.ID)
	if err != nil {
		return store.VirtualAddress{}, err
	}
	used := make(map[string]bool, len(addresses))
	usedHostnames := make(map[string]bool, len(addresses))
	for _, address := range addresses {
		used[strings.TrimSpace(address.VirtualIP)] = true
		if strings.TrimSpace(address.Hostname) != "" {
			usedHostnames[strings.TrimSpace(address.Hostname)] = true
		}
	}
	for ip := nextIPv4(base); ipNet.Contains(ip); ip = nextIPv4(ip) {
		if isIPv4Broadcast(ipNet, ip) || used[ip.String()] {
			continue
		}
		addressHostname := hostname
		if strings.TrimSpace(addressHostname) == "" || usedHostnames[strings.TrimSpace(addressHostname)] {
			addressHostname = deviceID
		}
		address := store.VirtualAddress{
			DeviceID: deviceID, NetworkID: network.ID, VirtualIP: ip.String(),
			Hostname: addressHostname, DNSEnabled: false,
		}
		if err := db.UpsertVirtualAddress(ctx, address); err != nil {
			if strings.TrimSpace(addressHostname) != deviceID {
				address.Hostname = deviceID
				if retryErr := db.UpsertVirtualAddress(ctx, address); retryErr == nil {
					return address, nil
				}
			}
			return store.VirtualAddress{}, err
		}
		return address, nil
	}
	return store.VirtualAddress{}, badRequest("lan_network_full", "no available virtual ip")
}

func nextIPv4(ip net.IP) net.IP {
	out := append(net.IP(nil), ip.To4()...)
	for i := len(out) - 1; i >= 0; i-- {
		out[i]++
		if out[i] != 0 {
			break
		}
	}
	return out
}

func isIPv4Broadcast(n *net.IPNet, ip net.IP) bool {
	v := ip.To4()
	base := n.IP.To4()
	if v == nil || base == nil {
		return false
	}
	for i := range v {
		if v[i] != base[i]|^n.Mask[i] {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (a *App) handleLANStatus(w http.ResponseWriter, r *http.Request) {
	var req store.VirtualPeerState
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DeviceID) == "" || req.NetworkID <= 0 {
		writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
		return
	}
	err := a.db.PutVirtualPeerState(r.Context(), req)
	writeJSONOrError(w, map[string]any{"ok": true}, err)
}

func decodeVirtualNetwork(r *http.Request) (store.VirtualNetwork, error) {
	var network store.VirtualNetwork
	if err := json.NewDecoder(r.Body).Decode(&network); err != nil {
		return network, badRequest("bad_json", "bad json")
	}
	network.Name = strings.TrimSpace(network.Name)
	network.CIDR = strings.TrimSpace(network.CIDR)
	network.PathPolicy = strings.TrimSpace(network.PathPolicy)
	if network.Name == "" || network.CIDR == "" {
		return network, badRequest("bad_network", "name and cidr are required")
	}
	if _, _, err := net.ParseCIDR(network.CIDR); err != nil {
		return network, badRequest("bad_network", "cidr is invalid")
	}
	if network.MTU < 0 || network.MTU > 9000 || (network.MTU > 0 && network.MTU < 576) {
		return network, badRequest("bad_network", "mtu must be 0 or between 576 and 9000")
	}
	if network.MSS < 0 || network.MSS > vnet.DefaultMSS || (network.MSS > 0 && network.MSS < 536) {
		return network, badRequest("bad_network", "mss must be 0 or between 536 and 1200")
	}
	if network.MTU > 0 && network.MSS > 0 && network.MSS > vnet.MSSForMTU(network.MTU) {
		return network, badRequest("bad_network", "mss is too large for mtu")
	}
	switch network.PathPolicy {
	case "", "auto", "prefer_p2p", "prefer_relay", "relay_only":
		if network.PathPolicy == "" {
			network.PathPolicy = "prefer_p2p"
		}
	default:
		return network, badRequest("bad_network", "path_policy is invalid")
	}
	return network, nil
}

func validateVirtualAddress(address store.VirtualAddress) error {
	if strings.TrimSpace(address.DeviceID) == "" || address.NetworkID <= 0 || strings.TrimSpace(address.VirtualIP) == "" {
		return badRequest("bad_address", "device_id, network_id and virtual_ip are required")
	}
	if net.ParseIP(strings.TrimSpace(address.VirtualIP)) == nil {
		return badRequest("bad_address", "virtual_ip is invalid")
	}
	return nil
}

func decodeLANGroup(r *http.Request) (store.VirtualDeviceGroup, []string, error) {
	var req struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		DeviceIDs []string `json:"device_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return store.VirtualDeviceGroup{}, nil, badRequest("bad_json", "bad json")
	}
	group := store.VirtualDeviceGroup{ID: strings.TrimSpace(req.ID), Name: strings.TrimSpace(req.Name)}
	if group.ID == "" || group.Name == "" {
		return group, nil, badRequest("bad_group", "id and name are required")
	}
	seen := map[string]bool{}
	members := make([]string, 0, len(req.DeviceIDs))
	for _, deviceID := range req.DeviceIDs {
		deviceID = strings.TrimSpace(deviceID)
		if deviceID == "" || seen[deviceID] {
			continue
		}
		seen[deviceID] = true
		members = append(members, deviceID)
	}
	return group, members, nil
}

func (a *App) expandVirtualACLRules(ctx context.Context, rules []store.VirtualACLRule, addresses []store.VirtualAddress) ([]store.VirtualACLRule, error) {
	members, err := a.db.ListVirtualDeviceGroupMembers(ctx, "")
	if err != nil {
		return nil, err
	}
	byGroup := map[string][]string{}
	for _, member := range members {
		byGroup[member.GroupID] = append(byGroup[member.GroupID], member.DeviceID)
	}
	known := map[string]bool{}
	for _, address := range addresses {
		known[address.DeviceID] = true
	}
	out := make([]store.VirtualACLRule, 0, len(rules))
	for _, rule := range rules {
		srcs := aclDevices(rule.SourceDeviceID, rule.SourceGroupID, byGroup, known)
		dsts := aclDevices(rule.TargetDeviceID, rule.TargetGroupID, byGroup, known)
		for _, src := range srcs {
			for _, dst := range dsts {
				expanded := rule
				expanded.SourceDeviceID = src
				expanded.TargetDeviceID = dst
				expanded.SourceGroupID = ""
				expanded.TargetGroupID = ""
				out = append(out, expanded)
			}
		}
	}
	return out, nil
}

func aclDevices(deviceID, groupID string, byGroup map[string][]string, known map[string]bool) []string {
	if strings.TrimSpace(deviceID) != "" {
		return []string{strings.TrimSpace(deviceID)}
	}
	if strings.TrimSpace(groupID) != "" {
		return byGroup[strings.TrimSpace(groupID)]
	}
	out := make([]string, 0, len(known))
	for deviceID := range known {
		out = append(out, deviceID)
	}
	return out
}

func (a *App) validateVirtualAddressCIDR(ctx context.Context, address store.VirtualAddress) error {
	network, err := a.virtualNetworkByID(ctx, address.NetworkID)
	if err != nil {
		return err
	}
	_, ipNet, err := net.ParseCIDR(network.CIDR)
	if err != nil {
		return err
	}
	ip := net.ParseIP(strings.TrimSpace(address.VirtualIP)).To4()
	if ip == nil || !ipNet.Contains(ip) {
		return badRequest("address_out_of_cidr", "virtual_ip must be inside network cidr")
	}
	if ip.Equal(ipNet.IP.To4()) || isIPv4Broadcast(ipNet, ip) {
		return badRequest("address_reserved", "virtual_ip cannot be network or broadcast address")
	}
	return nil
}

func (a *App) virtualNetworkByID(ctx context.Context, id int64) (store.VirtualNetwork, error) {
	networks, err := a.db.ListVirtualNetworks(ctx)
	if err != nil {
		return store.VirtualNetwork{}, err
	}
	for _, network := range networks {
		if network.ID == id {
			return network, nil
		}
	}
	return store.VirtualNetwork{}, sql.ErrNoRows
}

func (a *App) touchVirtualAddress(ctx context.Context, networkID int64, deviceID string) error {
	address, err := a.db.GetVirtualAddress(ctx, networkID, deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		network, err := a.virtualNetworkByID(ctx, networkID)
		if err != nil {
			return err
		}
		_, err = a.allocateVirtualAddress(ctx, network, deviceID, "")
		return err
	}
	if err != nil {
		return err
	}
	return a.db.UpsertVirtualAddress(ctx, address)
}

func decodeVirtualACLRule(r *http.Request) (store.VirtualACLRule, error) {
	var rule store.VirtualACLRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		return rule, badRequest("bad_json", "bad json")
	}
	rule.Protocol = strings.ToLower(strings.TrimSpace(rule.Protocol))
	rule.Action = strings.ToLower(strings.TrimSpace(rule.Action))
	if rule.Protocol == "" {
		rule.Protocol = "tcp"
	}
	if rule.Action == "" {
		rule.Action = "allow"
	}
	if rule.NetworkID <= 0 {
		return rule, badRequest("bad_acl", "network_id is required")
	}
	if rule.Protocol != "tcp" && rule.Protocol != "udp" && rule.Protocol != "icmp" && rule.Protocol != "any" {
		return rule, badRequest("bad_acl", "protocol must be tcp, udp, icmp or any")
	}
	if rule.Action != "allow" && rule.Action != "deny" {
		return rule, badRequest("bad_acl", "action must be allow or deny")
	}
	if rule.PortStart < 0 || rule.PortEnd < 0 || rule.PortStart > 65535 || rule.PortEnd > 65535 || rule.PortStart > rule.PortEnd {
		return rule, badRequest("bad_acl", "invalid port range")
	}
	return rule, nil
}

func (a *App) decodeVirtualRoute(r *http.Request) (store.VirtualRoute, error) {
	var route store.VirtualRoute
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		return route, badRequest("bad_json", "bad json")
	}
	route.DeviceID = strings.TrimSpace(route.DeviceID)
	route.CIDR = strings.TrimSpace(route.CIDR)
	if route.NetworkID <= 0 || route.DeviceID == "" || route.CIDR == "" {
		return route, badRequest("bad_route", "network_id, device_id and cidr are required")
	}
	if err := validateSubnetAdvertisement(route.CIDR); err != nil {
		return route, err
	}
	if _, err := a.db.GetDevice(r.Context(), route.DeviceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return route, badRequest("device_not_found", "device not found")
		}
		return route, err
	}
	return route, nil
}

func validateSubnetAdvertisement(cidr string) error {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil || ip.To4() == nil || ipNet == nil {
		return badRequest("bad_route", "cidr must be an ipv4 subnet")
	}
	if cidr == "0.0.0.0/0" {
		return badRequest("default_route_forbidden", "default route is not supported")
	}
	return nil
}

func parseLANPathID(path, prefix string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(path, prefix), 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest("bad_id", "bad id")
	}
	return id, nil
}

func optionalInt64Query(r *http.Request, key string) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, badRequest("bad_"+key, "bad "+key)
	}
	return id, nil
}

func optionalIntQuery(r *http.Request, key string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, badRequest("bad_"+key, "bad "+key)
	}
	return value, nil
}

func requiredInt64Query(r *http.Request, key string) (int64, error) {
	value, err := optionalInt64Query(r, key)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, badRequest("missing_"+key, key+" is required")
	}
	return value, nil
}

func lanConfigVersion(network store.VirtualNetwork, addresses []store.VirtualAddress, acl []store.VirtualACLRule, routes []store.VirtualRoute) string {
	parts := []string{network.UpdatedAt, strconv.FormatInt(network.ID, 10)}
	for _, address := range addresses {
		parts = append(parts, address.UpdatedAt)
	}
	for _, rule := range acl {
		parts = append(parts, rule.UpdatedAt)
	}
	for _, route := range routes {
		parts = append(parts, route.UpdatedAt)
	}
	return strings.Join(parts, "|")
}
