package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"udp_tunnel_demo/internal/store"
)

const lanBootstrapVersion = 1

type lanBootstrapResponse struct {
	Version       int                    `json:"version"`
	Capabilities  []string               `json:"capabilities"`
	ConfigVersion string                 `json:"config_version"`
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
		err := a.db.DeleteVirtualNetwork(r.Context(), id)
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
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
		err := a.db.UpsertVirtualAddress(r.Context(), address)
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
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
	network, err := a.db.EnsureDefaultVirtualNetwork(r.Context())
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if err := a.db.UpsertDevice(r.Context(), req.DeviceID, strings.TrimSpace(req.DeviceName), requestAddr(r), "", "", false); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	address, err := a.db.GetVirtualAddress(r.Context(), network.ID, req.DeviceID)
	if errors.Is(err, sql.ErrNoRows) {
		address = store.VirtualAddress{DeviceID: req.DeviceID, NetworkID: network.ID}
	} else if err != nil {
		writeJSONOrError(w, nil, err)
		return
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
	peers := make([]lanBootstrapPeer, 0, len(addresses))
	for _, peer := range addresses {
		if peer.DeviceID == req.DeviceID {
			continue
		}
		peers = append(peers, lanBootstrapPeer{DeviceID: peer.DeviceID, VirtualIP: peer.VirtualIP, Hostname: peer.Hostname})
	}
	writeJSON(w, http.StatusOK, lanBootstrapResponse{
		Version: lanBootstrapVersion, Capabilities: []string{"ipv4", "tcp", "rdp"},
		ConfigVersion: lanConfigVersion(network, addresses, acl, routes), DeviceID: req.DeviceID,
		DeviceName: strings.TrimSpace(req.DeviceName), Network: network, Address: address, Routes: routes, ACL: acl, Peers: peers,
	})
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
	if network.Name == "" || network.CIDR == "" {
		return network, badRequest("bad_network", "name and cidr are required")
	}
	if _, _, err := net.ParseCIDR(network.CIDR); err != nil {
		return network, badRequest("bad_network", "cidr is invalid")
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
