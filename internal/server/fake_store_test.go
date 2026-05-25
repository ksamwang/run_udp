package server

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"udp_tunnel_demo/internal/store"
)

type fakeStore struct {
	mu             sync.Mutex
	meta           map[string]string
	devices        map[string]store.Device
	productStates  map[string]store.DeviceProductState
	rules          map[int64]store.ForwardRule
	nextRuleID     int64
	sessions       map[int64]store.Session
	nextSessionID  int64
	tunnelStates   map[string]store.TunnelState
	refreshTokens  map[string]store.AdminRefreshToken
	adminUsers     map[string]store.AdminUser
	virtualNets    map[int64]store.VirtualNetwork
	virtualAddrs   map[string]store.VirtualAddress
	virtualKeys    map[string]store.VirtualDeviceKey
	virtualGroups  map[string]store.VirtualDeviceGroup
	virtualMembers map[string]store.VirtualDeviceGroupMember
	virtualACLs    map[int64]store.VirtualACLRule
	virtualRoutes  map[string]store.VirtualRoute
	virtualPeers   map[string]store.VirtualPeerState
	virtualEvents  []store.VirtualPeerPathEvent
	learnedPaths   map[string]store.VirtualLearnedPath
	nextLANID      int64
	nextLANEventID int64
	auditEvents    []store.AuditEvent
	nextAuditID    int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		meta:           map[string]string{},
		devices:        map[string]store.Device{},
		productStates:  map[string]store.DeviceProductState{},
		rules:          map[int64]store.ForwardRule{},
		sessions:       map[int64]store.Session{},
		tunnelStates:   map[string]store.TunnelState{},
		refreshTokens:  map[string]store.AdminRefreshToken{},
		adminUsers:     map[string]store.AdminUser{},
		virtualNets:    map[int64]store.VirtualNetwork{},
		virtualAddrs:   map[string]store.VirtualAddress{},
		virtualKeys:    map[string]store.VirtualDeviceKey{},
		virtualGroups:  map[string]store.VirtualDeviceGroup{},
		virtualMembers: map[string]store.VirtualDeviceGroupMember{},
		virtualACLs:    map[int64]store.VirtualACLRule{},
		virtualRoutes:  map[string]store.VirtualRoute{},
		virtualPeers:   map[string]store.VirtualPeerState{},
		virtualEvents:  []store.VirtualPeerPathEvent{},
		learnedPaths:   map[string]store.VirtualLearnedPath{},
	}
}

func (s *fakeStore) Close() error { return nil }

func (s *fakeStore) PutMeta(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta[key] = value
	return nil
}

func (s *fakeStore) GetMeta(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta[key], nil
}

func (s *fakeStore) PutSystemSetting(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta["system_"+key] = value
	return nil
}

func (s *fakeStore) GetSystemSetting(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta["system_"+key], nil
}

func (s *fakeStore) UpsertDevice(ctx context.Context, id, name, addr, upnpAddr, want string, online bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowTestString()
	d, ok := s.devices[id]
	if !ok {
		d = store.Device{ID: id, Name: id, Enabled: true, CreatedAt: now}
	}
	if name != "" {
		d.Name = name
	}
	if addr != "" {
		d.Addr = addr
	}
	if upnpAddr != "" {
		d.UpnpAddr = upnpAddr
	}
	if want != "" {
		d.Want = want
	}
	d.Online = online
	d.LastSeen = now
	s.devices[id] = d
	return nil
}

func (s *fakeStore) UpsertDeviceProductState(ctx context.Context, state store.DeviceProductState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(state.DeviceID) == "" || strings.TrimSpace(state.Product) == "" {
		return nil
	}
	if state.LastSeenAt == "" {
		state.LastSeenAt = nowTestString()
	}
	key := state.DeviceID + "\x00" + state.Product
	s.productStates[key] = state
	return nil
}

func (s *fakeStore) ListDeviceProductStates(ctx context.Context) ([]store.DeviceProductState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.DeviceProductState, 0, len(s.productStates))
	for _, state := range s.productStates {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DeviceID == out[j].DeviceID {
			return out[i].Product < out[j].Product
		}
		return out[i].DeviceID < out[j].DeviceID
	})
	return out, nil
}

func (s *fakeStore) MarkOfflineBefore(ctx context.Context, cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, d := range s.devices {
		if d.Online && d.LastSeen < cutoff.Format(time.RFC3339) {
			d.Online = false
			s.devices[id] = d
		}
	}
	return nil
}

func (s *fakeStore) ListDevices(ctx context.Context) ([]store.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.Device, 0, len(s.devices))
	for _, d := range s.devices {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *fakeStore) GetDevice(ctx context.Context, id string) (store.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[id]
	if !ok {
		return store.Device{}, sql.ErrNoRows
	}
	return d, nil
}

func (s *fakeStore) SetDeviceEnabled(ctx context.Context, id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[id]
	if !ok {
		return sql.ErrNoRows
	}
	d.Enabled = enabled
	s.devices[id] = d
	return nil
}

func (s *fakeStore) DeleteDevice(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.devices[id]; !ok {
		return sql.ErrNoRows
	}
	delete(s.devices, id)
	return nil
}

func (s *fakeStore) EnabledRuleReferenceCount(ctx context.Context, deviceID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.rules {
		if r.Enabled && (r.SourceID == deviceID || r.TargetID == deviceID) {
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) ListRules(ctx context.Context) ([]store.ForwardRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.ForwardRule, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (s *fakeStore) RulesForDevice(ctx context.Context, deviceID string) ([]store.ForwardRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.ForwardRule
	for _, r := range s.rules {
		if r.Enabled && (r.SourceID == deviceID || r.TargetID == deviceID) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *fakeStore) CreateRule(ctx context.Context, rule store.ForwardRule) (store.ForwardRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextRuleID++
	now := nowTestString()
	rule.ID = s.nextRuleID
	rule.Profile = store.NormalizeProfile(rule.Profile)
	rule.CreatedAt = now
	rule.UpdatedAt = now
	s.rules[rule.ID] = rule
	return rule, nil
}

func (s *fakeStore) UpdateRule(ctx context.Context, id int64, rule store.ForwardRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rules[id]; !ok {
		return sql.ErrNoRows
	}
	rule.ID = id
	rule.Profile = store.NormalizeProfile(rule.Profile)
	rule.UpdatedAt = nowTestString()
	s.rules[id] = rule
	return nil
}

func (s *fakeStore) DeleteRule(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rules[id]; !ok {
		return sql.ErrNoRows
	}
	delete(s.rules, id)
	return nil
}

func (s *fakeStore) StartSession(ctx context.Context, sourceID, targetID, profile, path string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSessionID++
	now := nowTestString()
	s.sessions[s.nextSessionID] = store.Session{
		ID: s.nextSessionID, SourceID: sourceID, TargetID: targetID,
		Profile: store.NormalizeProfile(profile), Path: path, StartedAt: now, LastSeen: now,
	}
	return s.nextSessionID, nil
}

func (s *fakeStore) TouchSession(ctx context.Context, id int64, relayBytes int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	session.RelayBytes += relayBytes
	session.LastSeen = nowTestString()
	s.sessions[id] = session
	return nil
}

func (s *fakeStore) UpdateSessionPathForPair(ctx context.Context, aID, bID, profile, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest int64
	profile = store.NormalizeProfile(profile)
	for id, session := range s.sessions {
		if session.EndedAt != "" || session.Profile != profile {
			continue
		}
		if (session.SourceID == aID && session.TargetID == bID) || (session.SourceID == bID && session.TargetID == aID) {
			if id > latest {
				latest = id
			}
		}
	}
	if latest != 0 {
		session := s.sessions[latest]
		session.Path = path
		session.LastSeen = nowTestString()
		s.sessions[latest] = session
	}
	return nil
}

func (s *fakeStore) EndIdleSessions(ctx context.Context, cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, session := range s.sessions {
		if session.EndedAt == "" && session.LastSeen < cutoff.Format(time.RFC3339) {
			session.EndedAt = nowTestString()
			s.sessions[id] = session
		}
	}
	return nil
}

func (s *fakeStore) ListSessions(ctx context.Context) ([]store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		out = append(out, session)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (s *fakeStore) Metrics(ctx context.Context) (store.Metrics, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var m store.Metrics
	m.Devices = len(s.devices)
	for _, d := range s.devices {
		if d.Online {
			m.OnlineDevices++
		}
	}
	m.ForwardRules = len(s.rules)
	for _, session := range s.sessions {
		if session.EndedAt == "" {
			m.ActiveSessions++
		}
		m.RelayBytes += session.RelayBytes
	}
	return m, nil
}

func (s *fakeStore) PutTunnelState(ctx context.Context, ts store.TunnelState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts.Profile = store.NormalizeProfile(ts.Profile)
	if ts.LastTransitionAt == "" {
		ts.LastTransitionAt = nowTestString()
	}
	ts.UpdatedAt = nowTestString()
	s.tunnelStates[tunnelStateKey(ts.DeviceID, ts.PeerID, ts.Profile)] = ts
	return nil
}

func (s *fakeStore) ListTunnelStates(ctx context.Context) ([]store.TunnelState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.TunnelState, 0, len(s.tunnelStates))
	for _, ts := range s.tunnelStates {
		out = append(out, ts)
	}
	return out, nil
}

func (s *fakeStore) LocalPortConflict(ctx context.Context, sourceID string, localPort int, excludeID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rules {
		if r.Enabled && r.SourceID == sourceID && r.LocalPort == localPort && r.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeStore) Audit(ctx context.Context, kind, detail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextAuditID++
	s.auditEvents = append(s.auditEvents, store.AuditEvent{
		ID:        s.nextAuditID,
		Kind:      kind,
		Detail:    detail,
		CreatedAt: nowTestString(),
	})
	return nil
}

func (s *fakeStore) ListAuditEvents(ctx context.Context, filter store.AuditFilter) ([]store.AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var out []store.AuditEvent
	for i := len(s.auditEvents) - 1; i >= 0; i-- {
		event := s.auditEvents[i]
		if filter.Kind != "" && event.Kind != filter.Kind {
			continue
		}
		if filter.From != "" && event.CreatedAt < filter.From {
			continue
		}
		if filter.To != "" && event.CreatedAt > filter.To {
			continue
		}
		if filter.Keyword != "" && !strings.Contains(event.Kind, filter.Keyword) && !strings.Contains(event.Detail, filter.Keyword) {
			continue
		}
		out = append(out, event)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *fakeStore) UpsertAdminUser(ctx context.Context, user store.AdminUser) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if user.CreatedAt == "" {
		user.CreatedAt = nowTestString()
	}
	user.UpdatedAt = nowTestString()
	s.adminUsers[user.ID] = user
	return nil
}

func (s *fakeStore) GetAdminUserByUsername(ctx context.Context, username string) (store.AdminUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.adminUsers {
		if user.Username == username {
			return user, nil
		}
	}
	return store.AdminUser{}, sql.ErrNoRows
}

func (s *fakeStore) GetAdminUserByID(ctx context.Context, id string) (store.AdminUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.adminUsers[id]
	if !ok {
		return store.AdminUser{}, sql.ErrNoRows
	}
	return user, nil
}

func (s *fakeStore) UpdateAdminPassword(ctx context.Context, userID, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.adminUsers[userID]
	if !ok {
		return sql.ErrNoRows
	}
	user.PasswordHash = passwordHash
	user.PasswordVersion++
	user.UpdatedAt = nowTestString()
	s.adminUsers[userID] = user
	return nil
}

func (s *fakeStore) ClearAdminPasswordChangeRequired(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.adminUsers[userID]
	if !ok {
		return sql.ErrNoRows
	}
	user.ForcePasswordChange = false
	user.UpdatedAt = nowTestString()
	s.adminUsers[userID] = user
	return nil
}

func (s *fakeStore) CreateAdminRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time, userAgent, ip string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshTokens[tokenHash] = store.AdminRefreshToken{
		ID: int64(len(s.refreshTokens) + 1), UserID: userID, TokenHash: tokenHash,
		ExpiresAt: expiresAt.Format(time.RFC3339), CreatedAt: nowTestString(), UserAgent: userAgent, IP: ip,
	}
	return nil
}

func (s *fakeStore) GetAdminRefreshToken(ctx context.Context, tokenHash string) (store.AdminRefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.refreshTokens[tokenHash]
	if !ok {
		return store.AdminRefreshToken{}, sql.ErrNoRows
	}
	return token, nil
}

func (s *fakeStore) TouchAdminRefreshToken(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, token := range s.refreshTokens {
		if token.ID == id {
			token.LastUsedAt = nowTestString()
			s.refreshTokens[hash] = token
			return nil
		}
	}
	return nil
}

func (s *fakeStore) RevokeAdminRefreshToken(ctx context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := s.refreshTokens[tokenHash]
	token.RevokedAt = nowTestString()
	s.refreshTokens[tokenHash] = token
	return nil
}

func (s *fakeStore) RevokeAdminRefreshTokensByUser(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, token := range s.refreshTokens {
		if token.UserID == userID && token.RevokedAt == "" {
			token.RevokedAt = nowTestString()
			s.refreshTokens[hash] = token
		}
	}
	return nil
}

func (s *fakeStore) RevokeExpiredAdminRefreshTokens(ctx context.Context, cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, token := range s.refreshTokens {
		if token.RevokedAt == "" && token.ExpiresAt < cutoff.Format(time.RFC3339) {
			token.RevokedAt = nowTestString()
			s.refreshTokens[hash] = token
		}
	}
	return nil
}

func (s *fakeStore) EnsureDefaultVirtualNetwork(ctx context.Context) (store.VirtualNetwork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, network := range s.virtualNets {
		if network.CIDR == "172.16.10.0/24" {
			return network, nil
		}
	}
	s.nextLANID++
	now := nowTestString()
	network := store.VirtualNetwork{ID: s.nextLANID, Name: "Default Network", CIDR: "172.16.10.0/24", Enabled: true, CreatedAt: now, UpdatedAt: now}
	s.virtualNets[network.ID] = network
	return network, nil
}

func (s *fakeStore) ListVirtualNetworks(ctx context.Context) ([]store.VirtualNetwork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.VirtualNetwork, 0, len(s.virtualNets))
	for _, network := range s.virtualNets {
		out = append(out, network)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *fakeStore) CreateVirtualNetwork(ctx context.Context, network store.VirtualNetwork) (store.VirtualNetwork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextLANID++
	now := nowTestString()
	network.ID = s.nextLANID
	network.CreatedAt = now
	network.UpdatedAt = now
	s.virtualNets[network.ID] = network
	return network, nil
}

func (s *fakeStore) UpdateVirtualNetwork(ctx context.Context, id int64, network store.VirtualNetwork) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.virtualNets[id]; !ok {
		return sql.ErrNoRows
	}
	network.ID = id
	network.UpdatedAt = nowTestString()
	s.virtualNets[id] = network
	return nil
}

func (s *fakeStore) DeleteVirtualNetwork(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.virtualNets[id]; !ok {
		return sql.ErrNoRows
	}
	delete(s.virtualNets, id)
	return nil
}

func (s *fakeStore) UpsertVirtualAddress(ctx context.Context, address store.VirtualAddress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowTestString()
	if address.CreatedAt == "" {
		address.CreatedAt = now
	}
	address.UpdatedAt = now
	s.virtualAddrs[virtualAddressKey(address.NetworkID, address.DeviceID)] = address
	return nil
}

func (s *fakeStore) ListVirtualAddresses(ctx context.Context, networkID int64) ([]store.VirtualAddress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.VirtualAddress
	for _, address := range s.virtualAddrs {
		if networkID == 0 || address.NetworkID == networkID {
			out = append(out, address)
		}
	}
	return out, nil
}

func (s *fakeStore) GetVirtualAddress(ctx context.Context, networkID int64, deviceID string) (store.VirtualAddress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	address, ok := s.virtualAddrs[virtualAddressKey(networkID, deviceID)]
	if !ok {
		return store.VirtualAddress{}, sql.ErrNoRows
	}
	return address, nil
}

func (s *fakeStore) GetVirtualAddressByDevice(ctx context.Context, deviceID string) (store.VirtualAddress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var newest store.VirtualAddress
	for _, address := range s.virtualAddrs {
		if address.DeviceID == deviceID && (newest.UpdatedAt == "" || address.UpdatedAt >= newest.UpdatedAt) {
			newest = address
		}
	}
	if newest.DeviceID == "" {
		return store.VirtualAddress{}, sql.ErrNoRows
	}
	return newest, nil
}

func (s *fakeStore) DeleteVirtualAddress(ctx context.Context, networkID int64, deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := virtualAddressKey(networkID, deviceID)
	if _, ok := s.virtualAddrs[key]; !ok {
		return sql.ErrNoRows
	}
	delete(s.virtualAddrs, key)
	return nil
}

func (s *fakeStore) UpsertVirtualDeviceKey(ctx context.Context, key store.VirtualDeviceKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowTestString()
	if key.CreatedAt == "" {
		key.CreatedAt = now
	}
	key.UpdatedAt = now
	s.virtualKeys[key.DeviceID] = key
	return nil
}

func (s *fakeStore) GetVirtualDeviceKey(ctx context.Context, deviceID string) (store.VirtualDeviceKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.virtualKeys[deviceID]
	if !ok {
		return store.VirtualDeviceKey{}, sql.ErrNoRows
	}
	return key, nil
}

func (s *fakeStore) ListVirtualDeviceKeys(ctx context.Context) ([]store.VirtualDeviceKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.VirtualDeviceKey, 0, len(s.virtualKeys))
	for _, key := range s.virtualKeys {
		out = append(out, key)
	}
	return out, nil
}

func (s *fakeStore) UpsertVirtualDeviceGroup(ctx context.Context, group store.VirtualDeviceGroup) (store.VirtualDeviceGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowTestString()
	if group.CreatedAt == "" {
		group.CreatedAt = now
	}
	group.UpdatedAt = now
	s.virtualGroups[group.ID] = group
	return group, nil
}

func (s *fakeStore) DeleteVirtualDeviceGroup(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.virtualGroups[id]; !ok {
		return sql.ErrNoRows
	}
	delete(s.virtualGroups, id)
	for key, member := range s.virtualMembers {
		if member.GroupID == id {
			delete(s.virtualMembers, key)
		}
	}
	return nil
}

func (s *fakeStore) ListVirtualDeviceGroups(ctx context.Context) ([]store.VirtualDeviceGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.VirtualDeviceGroup, 0, len(s.virtualGroups))
	for _, group := range s.virtualGroups {
		out = append(out, group)
	}
	return out, nil
}

func (s *fakeStore) SetVirtualDeviceGroupMembers(ctx context.Context, groupID string, deviceIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, member := range s.virtualMembers {
		if member.GroupID == groupID {
			delete(s.virtualMembers, key)
		}
	}
	for _, deviceID := range deviceIDs {
		deviceID = strings.TrimSpace(deviceID)
		if deviceID == "" {
			continue
		}
		member := store.VirtualDeviceGroupMember{GroupID: groupID, DeviceID: deviceID, CreatedAt: nowTestString()}
		s.virtualMembers[groupID+"\x00"+deviceID] = member
	}
	return nil
}

func (s *fakeStore) ListVirtualDeviceGroupMembers(ctx context.Context, groupID string) ([]store.VirtualDeviceGroupMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.VirtualDeviceGroupMember
	for _, member := range s.virtualMembers {
		if groupID == "" || member.GroupID == groupID {
			out = append(out, member)
		}
	}
	return out, nil
}

func (s *fakeStore) CreateVirtualACLRule(ctx context.Context, rule store.VirtualACLRule) (store.VirtualACLRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextLANID++
	now := nowTestString()
	rule.ID = s.nextLANID
	rule.CreatedAt = now
	rule.UpdatedAt = now
	s.virtualACLs[rule.ID] = rule
	return rule, nil
}

func (s *fakeStore) ListVirtualACLRules(ctx context.Context, networkID int64) ([]store.VirtualACLRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.VirtualACLRule
	for _, rule := range s.virtualACLs {
		if networkID == 0 || rule.NetworkID == networkID {
			out = append(out, rule)
		}
	}
	return out, nil
}

func (s *fakeStore) UpdateVirtualACLRule(ctx context.Context, id int64, rule store.VirtualACLRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.virtualACLs[id]; !ok {
		return sql.ErrNoRows
	}
	rule.ID = id
	rule.UpdatedAt = nowTestString()
	s.virtualACLs[id] = rule
	return nil
}

func (s *fakeStore) DeleteVirtualACLRule(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.virtualACLs[id]; !ok {
		return sql.ErrNoRows
	}
	delete(s.virtualACLs, id)
	return nil
}

func (s *fakeStore) UpsertVirtualRoute(ctx context.Context, route store.VirtualRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowTestString()
	if route.ID > 0 {
		for key, existing := range s.virtualRoutes {
			if existing.ID == route.ID {
				route.CreatedAt = existing.CreatedAt
				route.UpdatedAt = now
				delete(s.virtualRoutes, key)
				s.virtualRoutes[virtualRouteKey(route.NetworkID, route.DeviceID, route.CIDR)] = route
				return nil
			}
		}
		return sql.ErrNoRows
	}
	if route.CreatedAt == "" {
		route.CreatedAt = now
	}
	route.UpdatedAt = now
	if route.ID == 0 {
		s.nextLANID++
		route.ID = s.nextLANID
	}
	s.virtualRoutes[virtualRouteKey(route.NetworkID, route.DeviceID, route.CIDR)] = route
	return nil
}

func (s *fakeStore) ListVirtualRoutes(ctx context.Context, networkID int64, deviceID string) ([]store.VirtualRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.VirtualRoute
	for _, route := range s.virtualRoutes {
		if (networkID == 0 || route.NetworkID == networkID) && (deviceID == "" || route.DeviceID == deviceID) {
			out = append(out, route)
		}
	}
	return out, nil
}

func (s *fakeStore) DeleteVirtualRoute(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, route := range s.virtualRoutes {
		if route.ID == id {
			delete(s.virtualRoutes, key)
			return nil
		}
	}
	return sql.ErrNoRows
}

func (s *fakeStore) PutVirtualPeerState(ctx context.Context, state store.VirtualPeerState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state.LastTransitionAt == "" {
		state.LastTransitionAt = nowTestString()
	}
	state.UpdatedAt = nowTestString()
	key := virtualPeerKey(state.NetworkID, state.DeviceID, state.PeerID)
	previous, hasPrevious := s.virtualPeers[key]
	s.virtualPeers[key] = state
	if !hasPrevious || previous.Path != state.Path || previous.DataPath != state.DataPath || previous.PathReason != state.PathReason {
		s.nextLANEventID++
		s.virtualEvents = append(s.virtualEvents, store.VirtualPeerPathEvent{
			ID: s.nextLANEventID, DeviceID: state.DeviceID, PeerID: state.PeerID, NetworkID: state.NetworkID,
			Path: state.Path, DataPath: state.DataPath, PathReason: state.PathReason, TrafficClass: state.TrafficClass,
			TxBytes: state.TxBytes, RxBytes: state.RxBytes, CreatedAt: state.UpdatedAt,
		})
	}
	return nil
}

func (s *fakeStore) ListVirtualPeerStates(ctx context.Context, networkID int64) ([]store.VirtualPeerState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.VirtualPeerState
	for _, state := range s.virtualPeers {
		if networkID == 0 || state.NetworkID == networkID {
			out = append(out, state)
		}
	}
	return out, nil
}

func (s *fakeStore) ListVirtualPeerPathEvents(ctx context.Context, networkID int64, deviceID, peerID string, limit int) ([]store.VirtualPeerPathEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	out := make([]store.VirtualPeerPathEvent, 0)
	for i := len(s.virtualEvents) - 1; i >= 0 && len(out) < limit; i-- {
		event := s.virtualEvents[i]
		if networkID > 0 && event.NetworkID != networkID {
			continue
		}
		if deviceID != "" && event.DeviceID != deviceID {
			continue
		}
		if peerID != "" && event.PeerID != peerID {
			continue
		}
		out = append(out, event)
	}
	return out, nil
}

func (s *fakeStore) UpsertVirtualLearnedPath(ctx context.Context, path store.VirtualLearnedPath) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := learnedPathKey(path.NetworkID, path.DeviceID, path.PeerID, path.DstPort)
	if path.Protocol == "" {
		path.Protocol = "tcp"
	}
	if path.Quality == "" {
		path.Quality = "ok"
	}
	if path.UpdatedAt == "" {
		path.UpdatedAt = nowTestString()
	}
	if path.LastSuccessAt == "" && path.SuccessCount > 0 {
		path.LastSuccessAt = path.UpdatedAt
	}
	if path.LastFailureAt == "" && path.FailureCount > 0 {
		path.LastFailureAt = path.UpdatedAt
	}
	s.learnedPaths[key] = path
	return nil
}

func (s *fakeStore) ListVirtualLearnedPaths(ctx context.Context, networkID int64, deviceID string) ([]store.VirtualLearnedPath, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deviceID = strings.TrimSpace(deviceID)
	out := make([]store.VirtualLearnedPath, 0)
	for _, path := range s.learnedPaths {
		if networkID > 0 && path.NetworkID != networkID {
			continue
		}
		if deviceID != "" && path.DeviceID != deviceID {
			continue
		}
		out = append(out, path)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

func (s *fakeStore) SetVirtualLearnedPathPreheat(ctx context.Context, networkID int64, deviceID, peerID string, dstPort int, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := learnedPathKey(networkID, deviceID, peerID, dstPort)
	path, ok := s.learnedPaths[key]
	if !ok {
		return sql.ErrNoRows
	}
	path.PreheatEnabled = enabled
	path.UpdatedAt = nowTestString()
	s.learnedPaths[key] = path
	return nil
}

func learnedPathKey(networkID int64, deviceID, peerID string, dstPort int) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%d", networkID, deviceID, peerID, dstPort)
}

func tunnelStateKey(deviceID, peerID, profile string) string {
	return deviceID + "\x00" + peerID + "\x00" + store.NormalizeProfile(profile)
}

func virtualAddressKey(networkID int64, deviceID string) string {
	return fmt.Sprintf("%d\x00%s", networkID, deviceID)
}

func virtualRouteKey(networkID int64, deviceID, cidr string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", networkID, deviceID, cidr)
}

func virtualPeerKey(networkID int64, deviceID, peerID string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", networkID, deviceID, peerID)
}

func nowTestString() string {
	return time.Now().Format(time.RFC3339)
}
