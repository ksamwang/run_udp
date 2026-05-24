package controlstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"udp_tunnel_demo/internal/store"
)

var ErrMissingDSN = errors.New("mysql control database dsn is required")

type Config struct {
	DSN string
}

type MySQLStore struct {
	db *gorm.DB
}

func Open(cfg Config) (Store, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, ErrMissingDSN
	}
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                      cfg.DSN,
		DisableDatetimePrecision: true,
		DontSupportRenameIndex:   true,
		DontSupportRenameColumn:  true,
		DefaultStringSize:        191,
	}), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	s := &MySQLStore{db: db}
	if err := s.AutoMigrate(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *MySQLStore) AutoMigrate() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.AutoMigrate(
		&Meta{},
		&SystemSetting{},
		&Device{},
		&ForwardRule{},
		&Session{},
		&AuditEvent{},
		&TunnelState{},
		&AdminUser{},
		&AdminRefreshToken{},
		&VirtualNetwork{},
		&VirtualAddress{},
		&VirtualDeviceKey{},
		&VirtualACLRule{},
		&VirtualRoute{},
		&VirtualPeerState{},
	)
}

func (s *MySQLStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

func (s *MySQLStore) PutMeta(ctx context.Context, key, value string) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]any{"value": value}),
	}).Create(&Meta{Key: key, Value: value}).Error
}

func (s *MySQLStore) GetMeta(ctx context.Context, key string) (string, error) {
	var m Meta
	err := s.db.WithContext(ctx).First(&m, "`key` = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return m.Value, err
}

func (s *MySQLStore) PutSystemSetting(ctx context.Context, key, value string) error {
	now := nowString()
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]any{"value": value, "updated_at": now}),
	}).Create(&SystemSetting{Key: key, Value: value, UpdatedAt: now}).Error
}

func (s *MySQLStore) GetSystemSetting(ctx context.Context, key string) (string, error) {
	var setting SystemSetting
	err := s.db.WithContext(ctx).First(&setting, "`key` = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return setting.Value, err
}

func (s *MySQLStore) UpsertDevice(ctx context.Context, id, name, addr, upnpAddr, want string, online bool) error {
	now := nowString()
	values := map[string]any{
		"online":    online,
		"last_seen": now,
	}
	if name != "" {
		values["name"] = name
	}
	if addr != "" {
		values["addr"] = addr
	}
	if upnpAddr != "" {
		values["upnp_addr"] = upnpAddr
	}
	if want != "" {
		values["want"] = want
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(values),
	}).Create(&Device{
		ID:        id,
		Name:      name,
		Addr:      addr,
		UpnpAddr:  upnpAddr,
		Want:      want,
		Online:    online,
		Enabled:   true,
		LastSeen:  now,
		CreatedAt: now,
	}).Error
}

func (s *MySQLStore) MarkOfflineBefore(ctx context.Context, cutoff time.Time) error {
	return s.db.WithContext(ctx).Model(&Device{}).
		Where("online = ? AND last_seen < ?", true, cutoff.Format(time.RFC3339)).
		Update("online", false).Error
}

func (s *MySQLStore) ListDevices(ctx context.Context) ([]store.Device, error) {
	var rows []Device
	if err := s.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]store.Device, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *MySQLStore) GetDevice(ctx context.Context, id string) (store.Device, error) {
	var row Device
	err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.Device{}, sql.ErrNoRows
	}
	return row.toStore(), err
}

func (s *MySQLStore) SetDeviceEnabled(ctx context.Context, id string, enabled bool) error {
	tx := s.db.WithContext(ctx).Model(&Device{}).Where("id = ?", id).Update("enabled", enabled)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) DeleteDevice(ctx context.Context, id string) error {
	tx := s.db.WithContext(ctx).Delete(&Device{ID: id})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) EnabledRuleReferenceCount(ctx context.Context, deviceID string) (int, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&ForwardRule{}).
		Where("enabled = ? AND (source_id = ? OR target_id = ?)", true, deviceID, deviceID).
		Count(&n).Error
	return int(n), err
}

func (s *MySQLStore) ListRules(ctx context.Context) ([]store.ForwardRule, error) {
	var rows []ForwardRule
	if err := s.db.WithContext(ctx).Order("id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rulesToStore(rows), nil
}

func (s *MySQLStore) RulesForDevice(ctx context.Context, deviceID string) ([]store.ForwardRule, error) {
	var rows []ForwardRule
	if err := s.db.WithContext(ctx).
		Where("enabled = ? AND (source_id = ? OR target_id = ?)", true, deviceID, deviceID).
		Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rulesToStore(rows), nil
}

func (s *MySQLStore) CreateRule(ctx context.Context, rule store.ForwardRule) (store.ForwardRule, error) {
	now := nowString()
	row := ForwardRule{
		Name: rule.Name, SourceID: rule.SourceID, TargetID: rule.TargetID,
		Profile: store.NormalizeProfile(rule.Profile), LocalPort: rule.LocalPort,
		TargetHost: rule.TargetHost, TargetPort: rule.TargetPort, Enabled: rule.Enabled,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return rule, err
	}
	rule.ID = row.ID
	rule.Profile = row.Profile
	rule.CreatedAt = row.CreatedAt
	rule.UpdatedAt = row.UpdatedAt
	return rule, nil
}

func (s *MySQLStore) UpdateRule(ctx context.Context, id int64, rule store.ForwardRule) error {
	values := map[string]any{
		"name": rule.Name, "source_id": rule.SourceID, "target_id": rule.TargetID,
		"profile": store.NormalizeProfile(rule.Profile), "local_port": rule.LocalPort,
		"target_host": rule.TargetHost, "target_port": rule.TargetPort,
		"enabled": rule.Enabled, "updated_at": nowString(),
	}
	tx := s.db.WithContext(ctx).Model(&ForwardRule{}).Where("id = ?", id).Updates(values)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) DeleteRule(ctx context.Context, id int64) error {
	tx := s.db.WithContext(ctx).Delete(&ForwardRule{}, id)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) StartSession(ctx context.Context, sourceID, targetID, profile, path string) (int64, error) {
	now := nowString()
	row := Session{SourceID: sourceID, TargetID: targetID, Profile: store.NormalizeProfile(profile), Path: path, StartedAt: now, LastSeen: now}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

func (s *MySQLStore) TouchSession(ctx context.Context, id int64, relayBytes int64) error {
	return s.db.WithContext(ctx).Model(&Session{}).Where("id = ?", id).Updates(map[string]any{
		"last_seen":   nowString(),
		"relay_bytes": gorm.Expr("relay_bytes + ?", relayBytes),
	}).Error
}

func (s *MySQLStore) UpdateSessionPathForPair(ctx context.Context, aID, bID, profile, path string) error {
	var row Session
	err := s.db.WithContext(ctx).
		Where("ended_at = '' AND profile = ? AND ((source_id = ? AND target_id = ?) OR (source_id = ? AND target_id = ?))",
			store.NormalizeProfile(profile), aID, bID, bID, aID).
		Order("id DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&Session{}).Where("id = ?", row.ID).
		Updates(map[string]any{"path": path, "last_seen": nowString()}).Error
}

func (s *MySQLStore) EndIdleSessions(ctx context.Context, cutoff time.Time) error {
	return s.db.WithContext(ctx).Model(&Session{}).
		Where("ended_at = '' AND last_seen < ?", cutoff.Format(time.RFC3339)).
		Update("ended_at", nowString()).Error
}

func (s *MySQLStore) ListSessions(ctx context.Context) ([]store.Session, error) {
	var rows []Session
	if err := s.db.WithContext(ctx).Order("id DESC").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]store.Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *MySQLStore) Metrics(ctx context.Context) (store.Metrics, error) {
	var m store.Metrics
	if err := s.count(ctx, &Device{}, nil, &m.Devices); err != nil {
		return m, err
	}
	if err := s.count(ctx, &Device{}, map[string]any{"online": true}, &m.OnlineDevices); err != nil {
		return m, err
	}
	if err := s.count(ctx, &ForwardRule{}, nil, &m.ForwardRules); err != nil {
		return m, err
	}
	if err := s.count(ctx, &Session{}, map[string]any{"ended_at": ""}, &m.ActiveSessions); err != nil {
		return m, err
	}
	err := s.db.WithContext(ctx).Model(&Session{}).Select("COALESCE(SUM(relay_bytes), 0)").Scan(&m.RelayBytes).Error
	return m, err
}

func (s *MySQLStore) PutTunnelState(ctx context.Context, ts store.TunnelState) error {
	now := nowString()
	if ts.LastTransitionAt == "" {
		ts.LastTransitionAt = now
	}
	row := TunnelState{
		DeviceID: ts.DeviceID, PeerID: ts.PeerID, Profile: store.NormalizeProfile(ts.Profile),
		State: ts.State, Via: ts.Via, NATType: ts.NATType, PublicAddr: ts.PublicAddr,
		ConvID: ts.ConvID, RTTMs: ts.RTTMs, LastError: ts.LastError, Attempt: ts.Attempt,
		NextRetryAt: ts.NextRetryAt, LastTransitionAt: ts.LastTransitionAt, UpdatedAt: now,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_id"}, {Name: "peer_id"}, {Name: "profile"}},
		DoUpdates: clause.Assignments(map[string]any{
			"state": row.State, "via": row.Via, "nat_type": row.NATType, "public_addr": row.PublicAddr,
			"conv_id": row.ConvID, "rtt_ms": row.RTTMs, "last_error": row.LastError, "attempt": row.Attempt,
			"next_retry_at": row.NextRetryAt, "last_transition_at": row.LastTransitionAt, "updated_at": row.UpdatedAt,
		}),
	}).Create(&row).Error
}

func (s *MySQLStore) ListTunnelStates(ctx context.Context) ([]store.TunnelState, error) {
	var rows []TunnelState
	if err := s.db.WithContext(ctx).Order("updated_at DESC").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]store.TunnelState, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *MySQLStore) LocalPortConflict(ctx context.Context, sourceID string, localPort int, excludeID int64) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&ForwardRule{}).
		Where("source_id = ? AND local_port = ? AND enabled = ? AND id <> ?", sourceID, localPort, true, excludeID).
		Count(&n).Error
	return n > 0, err
}

func (s *MySQLStore) Audit(ctx context.Context, kind, detail string) error {
	return s.db.WithContext(ctx).Create(&AuditEvent{Kind: kind, Detail: detail, CreatedAt: nowString()}).Error
}

func (s *MySQLStore) UpsertAdminUser(ctx context.Context, user store.AdminUser) error {
	now := nowString()
	if user.CreatedAt == "" {
		user.CreatedAt = now
	}
	user.UpdatedAt = now
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"username":              user.Username,
			"name":                  user.Name,
			"role":                  user.Role,
			"force_password_change": user.ForcePasswordChange,
			"password_version":      user.PasswordVersion,
			"password_hash":         user.PasswordHash,
			"updated_at":            user.UpdatedAt,
		}),
	}).Create(&AdminUser{
		ID:                  user.ID,
		Username:            user.Username,
		Name:                user.Name,
		Role:                user.Role,
		ForcePasswordChange: user.ForcePasswordChange,
		PasswordVersion:     user.PasswordVersion,
		PasswordHash:        user.PasswordHash,
		CreatedAt:           user.CreatedAt,
		UpdatedAt:           user.UpdatedAt,
	}).Error
}

func (s *MySQLStore) GetAdminUserByUsername(ctx context.Context, username string) (store.AdminUser, error) {
	var row AdminUser
	err := s.db.WithContext(ctx).First(&row, "username = ?", username).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.AdminUser{}, sql.ErrNoRows
	}
	return row.toStore(), err
}

func (s *MySQLStore) GetAdminUserByID(ctx context.Context, id string) (store.AdminUser, error) {
	var row AdminUser
	err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.AdminUser{}, sql.ErrNoRows
	}
	return row.toStore(), err
}

func (s *MySQLStore) UpdateAdminPassword(ctx context.Context, userID, passwordHash string) error {
	tx := s.db.WithContext(ctx).Model(&AdminUser{}).Where("id = ?", userID).
		Updates(map[string]any{"password_hash": passwordHash, "password_version": gorm.Expr("password_version + 1"), "updated_at": nowString()})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) ClearAdminPasswordChangeRequired(ctx context.Context, userID string) error {
	tx := s.db.WithContext(ctx).Model(&AdminUser{}).Where("id = ?", userID).
		Updates(map[string]any{"force_password_change": false, "updated_at": nowString()})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) CreateAdminRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time, userAgent, ip string) error {
	return s.db.WithContext(ctx).Create(&AdminRefreshToken{
		UserID: userID, TokenHash: tokenHash, ExpiresAt: expiresAt.Format(time.RFC3339),
		UserAgent: userAgent, IP: ip, CreatedAt: nowString(),
	}).Error
}

func (s *MySQLStore) GetAdminRefreshToken(ctx context.Context, tokenHash string) (store.AdminRefreshToken, error) {
	var row AdminRefreshToken
	err := s.db.WithContext(ctx).First(&row, "token_hash = ?", tokenHash).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.AdminRefreshToken{}, sql.ErrNoRows
	}
	return row.toStore(), err
}

func (s *MySQLStore) TouchAdminRefreshToken(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Model(&AdminRefreshToken{}).Where("id = ?", id).Update("last_used_at", nowString()).Error
}

func (s *MySQLStore) RevokeAdminRefreshToken(ctx context.Context, tokenHash string) error {
	return s.db.WithContext(ctx).Model(&AdminRefreshToken{}).
		Where("token_hash = ? AND revoked_at = ''", tokenHash).
		Update("revoked_at", nowString()).Error
}

func (s *MySQLStore) RevokeAdminRefreshTokensByUser(ctx context.Context, userID string) error {
	return s.db.WithContext(ctx).Model(&AdminRefreshToken{}).
		Where("user_id = ? AND revoked_at = ''", userID).
		Update("revoked_at", nowString()).Error
}

func (s *MySQLStore) RevokeExpiredAdminRefreshTokens(ctx context.Context, cutoff time.Time) error {
	return s.db.WithContext(ctx).Model(&AdminRefreshToken{}).
		Where("revoked_at = '' AND expires_at < ?", cutoff.Format(time.RFC3339)).
		Update("revoked_at", nowString()).Error
}

func (s *MySQLStore) EnsureDefaultVirtualNetwork(ctx context.Context) (store.VirtualNetwork, error) {
	now := nowString()
	row := VirtualNetwork{Name: "Default Network", CIDR: "172.16.10.0/24", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "cidr"}},
		DoNothing: true,
	}).Create(&row).Error; err != nil {
		return store.VirtualNetwork{}, err
	}
	var out VirtualNetwork
	if err := s.db.WithContext(ctx).First(&out, "cidr = ?", "172.16.10.0/24").Error; err != nil {
		return store.VirtualNetwork{}, err
	}
	return out.toStore(), nil
}

func (s *MySQLStore) ListVirtualNetworks(ctx context.Context) ([]store.VirtualNetwork, error) {
	var rows []VirtualNetwork
	if err := s.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]store.VirtualNetwork, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *MySQLStore) CreateVirtualNetwork(ctx context.Context, network store.VirtualNetwork) (store.VirtualNetwork, error) {
	now := nowString()
	row := VirtualNetwork{
		Name: network.Name, CIDR: network.CIDR, Enabled: network.Enabled,
		CreatedAt: now, UpdatedAt: now,
	}
	if row.Name == "" {
		row.Name = row.CIDR
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return network, err
	}
	return row.toStore(), nil
}

func (s *MySQLStore) UpdateVirtualNetwork(ctx context.Context, id int64, network store.VirtualNetwork) error {
	values := map[string]any{
		"name": network.Name, "cidr": network.CIDR, "enabled": network.Enabled, "updated_at": nowString(),
	}
	tx := s.db.WithContext(ctx).Model(&VirtualNetwork{}).Where("id = ?", id).Updates(values)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) DeleteVirtualNetwork(ctx context.Context, id int64) error {
	tx := s.db.WithContext(ctx).Delete(&VirtualNetwork{}, id)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) UpsertVirtualAddress(ctx context.Context, address store.VirtualAddress) error {
	now := nowString()
	if address.CreatedAt == "" {
		address.CreatedAt = now
	}
	address.UpdatedAt = now
	hostname := nullableString(address.Hostname)
	row := VirtualAddress{
		DeviceID: address.DeviceID, NetworkID: address.NetworkID, VirtualIP: address.VirtualIP,
		Hostname: hostname, DNSEnabled: address.DNSEnabled,
		CreatedAt: address.CreatedAt, UpdatedAt: address.UpdatedAt,
	}
	var existing VirtualAddress
	err := s.db.WithContext(ctx).First(&existing, "device_id = ? AND network_id = ?", address.DeviceID, address.NetworkID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.db.WithContext(ctx).Create(&row).Error
	}
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&VirtualAddress{}).
		Where("device_id = ? AND network_id = ?", address.DeviceID, address.NetworkID).
		Updates(map[string]any{
			"virtual_ip": row.VirtualIP, "hostname": row.Hostname, "dns_enabled": row.DNSEnabled, "updated_at": row.UpdatedAt,
		}).Error
}

func (s *MySQLStore) ListVirtualAddresses(ctx context.Context, networkID int64) ([]store.VirtualAddress, error) {
	var rows []VirtualAddress
	q := s.db.WithContext(ctx).Order("network_id, virtual_ip")
	if networkID > 0 {
		q = q.Where("network_id = ?", networkID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]store.VirtualAddress, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *MySQLStore) GetVirtualAddress(ctx context.Context, networkID int64, deviceID string) (store.VirtualAddress, error) {
	var row VirtualAddress
	err := s.db.WithContext(ctx).First(&row, "network_id = ? AND device_id = ?", networkID, deviceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.VirtualAddress{}, sql.ErrNoRows
	}
	return row.toStore(), err
}

func (s *MySQLStore) UpsertVirtualDeviceKey(ctx context.Context, key store.VirtualDeviceKey) error {
	now := nowString()
	if key.CreatedAt == "" {
		key.CreatedAt = now
	}
	key.UpdatedAt = now
	row := VirtualDeviceKey{
		DeviceID: key.DeviceID, Algorithm: key.Algorithm, PublicKey: key.PublicKey,
		CreatedAt: key.CreatedAt, UpdatedAt: key.UpdatedAt,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"algorithm": row.Algorithm, "public_key": row.PublicKey, "updated_at": row.UpdatedAt,
		}),
	}).Create(&row).Error
}

func (s *MySQLStore) GetVirtualDeviceKey(ctx context.Context, deviceID string) (store.VirtualDeviceKey, error) {
	var row VirtualDeviceKey
	err := s.db.WithContext(ctx).First(&row, "device_id = ?", deviceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.VirtualDeviceKey{}, sql.ErrNoRows
	}
	return row.toStore(), err
}

func (s *MySQLStore) ListVirtualDeviceKeys(ctx context.Context) ([]store.VirtualDeviceKey, error) {
	var rows []VirtualDeviceKey
	if err := s.db.WithContext(ctx).Order("device_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]store.VirtualDeviceKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *MySQLStore) CreateVirtualACLRule(ctx context.Context, rule store.VirtualACLRule) (store.VirtualACLRule, error) {
	now := nowString()
	row := VirtualACLRule{
		NetworkID: rule.NetworkID, SourceDeviceID: rule.SourceDeviceID, SourceGroupID: rule.SourceGroupID,
		TargetDeviceID: rule.TargetDeviceID, TargetGroupID: rule.TargetGroupID, Protocol: rule.Protocol,
		PortStart: rule.PortStart, PortEnd: rule.PortEnd, Action: rule.Action, Enabled: rule.Enabled,
		CreatedAt: now, UpdatedAt: now,
	}
	if row.Protocol == "" {
		row.Protocol = "tcp"
	}
	if row.Action == "" {
		row.Action = "allow"
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return rule, err
	}
	return row.toStore(), nil
}

func (s *MySQLStore) ListVirtualACLRules(ctx context.Context, networkID int64) ([]store.VirtualACLRule, error) {
	var rows []VirtualACLRule
	q := s.db.WithContext(ctx).Order("id DESC")
	if networkID > 0 {
		q = q.Where("network_id = ?", networkID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]store.VirtualACLRule, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *MySQLStore) UpdateVirtualACLRule(ctx context.Context, id int64, rule store.VirtualACLRule) error {
	values := map[string]any{
		"network_id": rule.NetworkID, "source_device_id": rule.SourceDeviceID, "source_group_id": rule.SourceGroupID,
		"target_device_id": rule.TargetDeviceID, "target_group_id": rule.TargetGroupID, "protocol": rule.Protocol,
		"port_start": rule.PortStart, "port_end": rule.PortEnd, "action": rule.Action,
		"enabled": rule.Enabled, "updated_at": nowString(),
	}
	tx := s.db.WithContext(ctx).Model(&VirtualACLRule{}).Where("id = ?", id).Updates(values)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) DeleteVirtualACLRule(ctx context.Context, id int64) error {
	tx := s.db.WithContext(ctx).Delete(&VirtualACLRule{}, id)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) UpsertVirtualRoute(ctx context.Context, route store.VirtualRoute) error {
	now := nowString()
	if route.CreatedAt == "" {
		route.CreatedAt = now
	}
	route.UpdatedAt = now
	row := VirtualRoute{
		ID: route.ID, DeviceID: route.DeviceID, NetworkID: route.NetworkID, CIDR: route.CIDR,
		Advertise: route.Advertise, Accept: route.Accept, CreatedAt: route.CreatedAt, UpdatedAt: route.UpdatedAt,
	}
	if route.ID > 0 {
		tx := s.db.WithContext(ctx).Model(&VirtualRoute{}).Where("id = ?", route.ID).Updates(map[string]any{
			"device_id": route.DeviceID, "network_id": route.NetworkID, "cidr": route.CIDR,
			"advertise": route.Advertise, "accept": route.Accept, "updated_at": route.UpdatedAt,
		})
		if tx.Error != nil {
			return tx.Error
		}
		if tx.RowsAffected == 0 {
			return sql.ErrNoRows
		}
		return nil
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_id"}, {Name: "network_id"}, {Name: "cidr"}},
		DoUpdates: clause.Assignments(map[string]any{
			"advertise": row.Advertise, "accept": row.Accept, "updated_at": row.UpdatedAt,
		}),
	}).Create(&row).Error
}

func (s *MySQLStore) ListVirtualRoutes(ctx context.Context, networkID int64, deviceID string) ([]store.VirtualRoute, error) {
	var rows []VirtualRoute
	q := s.db.WithContext(ctx).Order("network_id, device_id, cidr")
	if networkID > 0 {
		q = q.Where("network_id = ?", networkID)
	}
	if deviceID != "" {
		q = q.Where("device_id = ?", deviceID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]store.VirtualRoute, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *MySQLStore) DeleteVirtualRoute(ctx context.Context, id int64) error {
	tx := s.db.WithContext(ctx).Delete(&VirtualRoute{}, id)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) PutVirtualPeerState(ctx context.Context, state store.VirtualPeerState) error {
	now := nowString()
	if state.LastTransitionAt == "" {
		state.LastTransitionAt = now
	}
	row := VirtualPeerState{
		DeviceID: state.DeviceID, PeerID: state.PeerID, NetworkID: state.NetworkID,
		State: state.State, Path: state.Path, AdapterState: state.AdapterState, RouteConflict: state.RouteConflict,
		SelectedCIDR: state.SelectedCIDR, MTU: state.MTU, MSS: state.MSS, RTTMs: state.RTTMs,
		TxBytes: state.TxBytes, RxBytes: state.RxBytes, DropReason: state.DropReason, LastError: state.LastError, LastHandshakeAt: state.LastHandshakeAt,
		LastTransitionAt: state.LastTransitionAt, UpdatedAt: now,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_id"}, {Name: "peer_id"}, {Name: "network_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"state": row.State, "path": row.Path, "adapter_state": row.AdapterState, "route_conflict": row.RouteConflict,
			"selected_cidr": row.SelectedCIDR, "mtu": row.MTU, "mss": row.MSS, "rtt_ms": row.RTTMs, "tx_bytes": row.TxBytes,
			"rx_bytes": row.RxBytes, "drop_reason": row.DropReason, "last_error": row.LastError,
			"last_handshake_at": row.LastHandshakeAt, "last_transition_at": row.LastTransitionAt, "updated_at": row.UpdatedAt,
		}),
	}).Create(&row).Error
}

func (s *MySQLStore) ListVirtualPeerStates(ctx context.Context, networkID int64) ([]store.VirtualPeerState, error) {
	var rows []VirtualPeerState
	q := s.db.WithContext(ctx).Order("updated_at DESC").Limit(200)
	if networkID > 0 {
		q = q.Where("network_id = ?", networkID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]store.VirtualPeerState, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *MySQLStore) count(ctx context.Context, model any, where map[string]any, dest *int) error {
	var n int64
	q := s.db.WithContext(ctx).Model(model)
	if where != nil {
		q = q.Where(where)
	}
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	*dest = int(n)
	return nil
}

func (d Device) toStore() store.Device {
	return store.Device{
		ID: d.ID, Name: d.Name, Addr: d.Addr, UpnpAddr: d.UpnpAddr, Want: d.Want,
		Online: d.Online, Enabled: d.Enabled, LastSeen: d.LastSeen, CreatedAt: d.CreatedAt,
	}
}

func (r ForwardRule) toStore() store.ForwardRule {
	return store.ForwardRule{
		ID: r.ID, Name: r.Name, SourceID: r.SourceID, TargetID: r.TargetID,
		Profile: store.NormalizeProfile(r.Profile), LocalPort: r.LocalPort,
		TargetHost: r.TargetHost, TargetPort: r.TargetPort, Enabled: r.Enabled,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func rulesToStore(rows []ForwardRule) []store.ForwardRule {
	out := make([]store.ForwardRule, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out
}

func (s Session) toStore() store.Session {
	return store.Session{
		ID: s.ID, SourceID: s.SourceID, TargetID: s.TargetID, Profile: store.NormalizeProfile(s.Profile),
		Path: s.Path, RelayBytes: s.RelayBytes, StartedAt: s.StartedAt, LastSeen: s.LastSeen, EndedAt: s.EndedAt,
	}
}

func (t TunnelState) toStore() store.TunnelState {
	return store.TunnelState{
		DeviceID: t.DeviceID, PeerID: t.PeerID, Profile: store.NormalizeProfile(t.Profile),
		State: t.State, Via: t.Via, NATType: t.NATType, PublicAddr: t.PublicAddr,
		ConvID: t.ConvID, RTTMs: t.RTTMs, LastError: t.LastError, Attempt: t.Attempt,
		NextRetryAt: t.NextRetryAt, LastTransitionAt: t.LastTransitionAt, UpdatedAt: t.UpdatedAt,
	}
}

func (t AdminRefreshToken) toStore() store.AdminRefreshToken {
	return store.AdminRefreshToken{
		ID: t.ID, UserID: t.UserID, TokenHash: t.TokenHash, ExpiresAt: t.ExpiresAt,
		RevokedAt: t.RevokedAt, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt,
		UserAgent: t.UserAgent, IP: t.IP,
	}
}

func (u AdminUser) toStore() store.AdminUser {
	return store.AdminUser{
		ID:                  u.ID,
		Username:            u.Username,
		Name:                u.Name,
		Role:                u.Role,
		ForcePasswordChange: u.ForcePasswordChange,
		PasswordVersion:     u.PasswordVersion,
		PasswordHash:        u.PasswordHash,
		CreatedAt:           u.CreatedAt,
		UpdatedAt:           u.UpdatedAt,
	}
}

func (n VirtualNetwork) toStore() store.VirtualNetwork {
	return store.VirtualNetwork{
		ID: n.ID, Name: n.Name, CIDR: n.CIDR, Enabled: n.Enabled, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
	}
}

func (a VirtualAddress) toStore() store.VirtualAddress {
	hostname := ""
	if a.Hostname != nil {
		hostname = *a.Hostname
	}
	return store.VirtualAddress{
		DeviceID: a.DeviceID, NetworkID: a.NetworkID, VirtualIP: a.VirtualIP, Hostname: hostname,
		DNSEnabled: a.DNSEnabled, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

func (k VirtualDeviceKey) toStore() store.VirtualDeviceKey {
	return store.VirtualDeviceKey{
		DeviceID: k.DeviceID, Algorithm: k.Algorithm, PublicKey: k.PublicKey, CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
	}
}

func (r VirtualACLRule) toStore() store.VirtualACLRule {
	return store.VirtualACLRule{
		ID: r.ID, NetworkID: r.NetworkID, SourceDeviceID: r.SourceDeviceID, SourceGroupID: r.SourceGroupID,
		TargetDeviceID: r.TargetDeviceID, TargetGroupID: r.TargetGroupID, Protocol: r.Protocol,
		PortStart: r.PortStart, PortEnd: r.PortEnd, Action: r.Action, Enabled: r.Enabled,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func (r VirtualRoute) toStore() store.VirtualRoute {
	return store.VirtualRoute{
		ID: r.ID, DeviceID: r.DeviceID, NetworkID: r.NetworkID, CIDR: r.CIDR,
		Advertise: r.Advertise, Accept: r.Accept, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func (p VirtualPeerState) toStore() store.VirtualPeerState {
	return store.VirtualPeerState{
		DeviceID: p.DeviceID, PeerID: p.PeerID, NetworkID: p.NetworkID, State: p.State, Path: p.Path,
		AdapterState: p.AdapterState, RouteConflict: p.RouteConflict, SelectedCIDR: p.SelectedCIDR,
		MTU: p.MTU, MSS: p.MSS, RTTMs: p.RTTMs, TxBytes: p.TxBytes, RxBytes: p.RxBytes, DropReason: p.DropReason,
		LastError: p.LastError, LastHandshakeAt: p.LastHandshakeAt, LastTransitionAt: p.LastTransitionAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func nowString() string {
	return time.Now().Format(time.RFC3339)
}

func nullableString(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

var _ Store = (*MySQLStore)(nil)
