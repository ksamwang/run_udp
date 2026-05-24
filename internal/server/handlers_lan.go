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

	"udp_tunnel_demo/internal/store"
)

const lanBootstrapVersion = 1

type lanBootstrapResponse struct {
	Version       int                    `json:"version"`
	Capabilities  []string               `json:"capabilities"`
	ConfigVersion string                 `json:"config_version"`
	Server        string                 `json:"server"`
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
	if strings.TrimSpace(req.PublicKey) != "" {
		if err := a.db.UpsertVirtualDeviceKey(r.Context(), store.VirtualDeviceKey{
			DeviceID: req.DeviceID, Algorithm: "ed25519", PublicKey: strings.TrimSpace(req.PublicKey),
		}); err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
	}
	address, err := a.db.GetVirtualAddress(r.Context(), network.ID, req.DeviceID)
	if errors.Is(err, sql.ErrNoRows) {
		address, err = a.allocateVirtualAddress(r.Context(), network, req.DeviceID, strings.TrimSpace(req.DeviceName))
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
	} else if err != nil {
		writeJSONOrError(w, nil, err)
		return
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
		ConfigVersion: lanConfigVersion(network, addresses, acl, routes), Server: externalUDPAddr(r, a.cfg.UDPListen), DeviceID: req.DeviceID,
		DeviceName: strings.TrimSpace(req.DeviceName), Network: network, Address: address, Routes: routes, ACL: acl, Peers: peers,
	})
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
	for _, address := range addresses {
		used[strings.TrimSpace(address.VirtualIP)] = true
	}
	for ip := nextIPv4(base); ipNet.Contains(ip); ip = nextIPv4(ip) {
		if isIPv4Broadcast(ipNet, ip) || used[ip.String()] {
			continue
		}
		address := store.VirtualAddress{
			DeviceID: deviceID, NetworkID: network.ID, VirtualIP: ip.String(),
			Hostname: hostname, DNSEnabled: false,
		}
		if err := db.UpsertVirtualAddress(ctx, address); err != nil {
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
