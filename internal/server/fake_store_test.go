package server

import (
	"context"
	"database/sql"
	"sort"
	"sync"
	"time"

	"udp_tunnel_demo/internal/store"
)

type fakeStore struct {
	mu            sync.Mutex
	meta          map[string]string
	devices       map[string]store.Device
	rules         map[int64]store.ForwardRule
	nextRuleID    int64
	sessions      map[int64]store.Session
	nextSessionID int64
	tunnelStates  map[string]store.TunnelState
	refreshTokens map[string]store.AdminRefreshToken
	adminUsers    map[string]store.AdminUser
	auditEvents   []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		meta:          map[string]string{},
		devices:       map[string]store.Device{},
		rules:         map[int64]store.ForwardRule{},
		sessions:      map[int64]store.Session{},
		tunnelStates:  map[string]store.TunnelState{},
		refreshTokens: map[string]store.AdminRefreshToken{},
		adminUsers:    map[string]store.AdminUser{},
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
	s.auditEvents = append(s.auditEvents, kind+":"+detail)
	return nil
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

func tunnelStateKey(deviceID, peerID, profile string) string {
	return deviceID + "\x00" + peerID + "\x00" + store.NormalizeProfile(profile)
}

func nowTestString() string {
	return time.Now().Format(time.RFC3339)
}
