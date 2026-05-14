package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Device struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Addr          string `json:"addr"`
	UpnpAddr      string `json:"upnp_addr,omitempty"`
	Want          string `json:"want,omitempty"`
	Online        bool   `json:"online"`
	Enabled       bool   `json:"enabled"`
	LastSeen      string `json:"last_seen"`
	CreatedAt     string `json:"created_at"`
	HealthSummary string `json:"health_summary,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

type ForwardRule struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	SourceID      string `json:"source_id"`
	TargetID      string `json:"target_id"`
	Profile       string `json:"profile"`
	LocalPort     int    `json:"local_port"`
	TargetHost    string `json:"target_host"`
	TargetPort    int    `json:"target_port"`
	Enabled       bool   `json:"enabled"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	RuntimeState  string `json:"runtime_state,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	LastUpdatedAt string `json:"last_updated_at,omitempty"`
	Attempt       int    `json:"attempt,omitempty"`
	NextRetryAt   string `json:"next_retry_at,omitempty"`
}

type Session struct {
	ID         int64  `json:"id"`
	SourceID   string `json:"source_id"`
	TargetID   string `json:"target_id"`
	Profile    string `json:"profile"`
	Path       string `json:"path"`
	RelayBytes int64  `json:"relay_bytes"`
	StartedAt  string `json:"started_at"`
	LastSeen   string `json:"last_seen"`
	EndedAt    string `json:"ended_at,omitempty"`
}

type Metrics struct {
	Devices        int   `json:"devices"`
	OnlineDevices  int   `json:"online_devices"`
	ForwardRules   int   `json:"forward_rules"`
	ActiveSessions int   `json:"active_sessions"`
	RelayBytes     int64 `json:"relay_bytes"`
}

type TunnelState struct {
	DeviceID         string `json:"device_id"`
	PeerID           string `json:"peer_id"`
	Profile          string `json:"profile"`
	State            string `json:"state"`
	Via              string `json:"via"`
	NATType          string `json:"nat_type"`
	PublicAddr       string `json:"public_addr"`
	ConvID           int64  `json:"conv_id"`
	RTTMs            int    `json:"rtt_ms"`
	LastError        string `json:"last_error"`
	Attempt          int    `json:"attempt"`
	NextRetryAt      string `json:"next_retry_at"`
	LastTransitionAt string `json:"last_transition_at"`
	UpdatedAt        string `json:"updated_at"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			addr TEXT NOT NULL DEFAULT '',
			upnp_addr TEXT NOT NULL DEFAULT '',
			want TEXT NOT NULL DEFAULT '',
			online INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_seen TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS forward_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			source_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			profile TEXT NOT NULL DEFAULT 'interactive',
			local_port INTEGER NOT NULL,
			target_host TEXT NOT NULL,
			target_port INTEGER NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			profile TEXT NOT NULL DEFAULT 'interactive',
			path TEXT NOT NULL,
			relay_bytes INTEGER NOT NULL DEFAULT 0,
			started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_seen TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ended_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			detail TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS tunnel_states (
			device_id TEXT NOT NULL,
			peer_id TEXT NOT NULL,
			profile TEXT NOT NULL DEFAULT 'interactive',
			state TEXT NOT NULL DEFAULT '',
			via TEXT NOT NULL DEFAULT '',
			nat_type TEXT NOT NULL DEFAULT '',
			public_addr TEXT NOT NULL DEFAULT '',
			conv_id INTEGER NOT NULL DEFAULT 0,
			rtt_ms INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			attempt INTEGER NOT NULL DEFAULT 0,
			next_retry_at TEXT NOT NULL DEFAULT '',
			last_transition_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (device_id, peer_id, profile)
		);`,
		`ALTER TABLE devices ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;`,
		`ALTER TABLE tunnel_states ADD COLUMN nat_type TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE tunnel_states ADD COLUMN rtt_ms INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE tunnel_states ADD COLUMN last_error TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE tunnel_states ADD COLUMN attempt INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE tunnel_states ADD COLUMN next_retry_at TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE tunnel_states ADD COLUMN last_transition_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP;`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	return nil
}

func (s *Store) PutMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// TouchDevice 只更新 online + last_seen，不动 addr/upnp/want。HTTP heartbeat 用。
// 避免覆盖 UDP register 路径上记录的真实地址信息。
func (s *Store) TouchDevice(ctx context.Context, id string, online bool) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices(id,name,online,enabled,last_seen)
		VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET online=excluded.online, last_seen=excluded.last_seen`,
		id, id, boolInt(online), 1, now)
	return err
}

func (s *Store) UpsertDevice(ctx context.Context, id, name, addr, upnpAddr, want string, online bool) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices(id,name,addr,upnp_addr,want,online,enabled,last_seen)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name=CASE WHEN excluded.name<>'' THEN excluded.name ELSE devices.name END,
			addr=CASE WHEN excluded.addr<>'' THEN excluded.addr ELSE devices.addr END,
			upnp_addr=CASE WHEN excluded.upnp_addr<>'' THEN excluded.upnp_addr ELSE devices.upnp_addr END,
			want=CASE WHEN excluded.want<>'' THEN excluded.want ELSE devices.want END,
			online=excluded.online, last_seen=excluded.last_seen`,
		id, name, addr, upnpAddr, want, boolInt(online), 1, now)
	return err
}

func (s *Store) MarkOfflineBefore(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET online=0 WHERE online=1 AND last_seen < ?`, cutoff.Format(time.RFC3339))
	return err
}

func (s *Store) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,addr,upnp_addr,want,online,enabled,last_seen,created_at FROM devices ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var online, enabled int
		if err := rows.Scan(&d.ID, &d.Name, &d.Addr, &d.UpnpAddr, &d.Want, &online, &enabled, &d.LastSeen, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Online = online != 0
		d.Enabled = enabled != 0
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDevice(ctx context.Context, id string) (Device, error) {
	var d Device
	var online, enabled int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,addr,upnp_addr,want,online,enabled,last_seen,created_at FROM devices WHERE id=?`, id).
		Scan(&d.ID, &d.Name, &d.Addr, &d.UpnpAddr, &d.Want, &online, &enabled, &d.LastSeen, &d.CreatedAt)
	d.Online = online != 0
	d.Enabled = enabled != 0
	return d, err
}

func (s *Store) SetDeviceEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET enabled=? WHERE id=?`, boolInt(enabled), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) EnabledRuleReferenceCount(ctx context.Context, deviceID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM forward_rules WHERE enabled=1 AND (source_id=? OR target_id=?)`, deviceID, deviceID).Scan(&n)
	return n, err
}

func (s *Store) ListRules(ctx context.Context) ([]ForwardRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,source_id,target_id,profile,local_port,target_host,target_port,enabled,created_at,updated_at FROM forward_rules ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRules(rows)
}

func (s *Store) RulesForDevice(ctx context.Context, deviceID string) ([]ForwardRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,source_id,target_id,profile,local_port,target_host,target_port,enabled,created_at,updated_at
		FROM forward_rules WHERE enabled=1 AND (source_id=? OR target_id=?) ORDER BY id`, deviceID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRules(rows)
}

func (s *Store) CreateRule(ctx context.Context, r ForwardRule) (ForwardRule, error) {
	r.Profile = NormalizeProfile(r.Profile)
	res, err := s.db.ExecContext(ctx, `INSERT INTO forward_rules(name,source_id,target_id,profile,local_port,target_host,target_port,enabled)
		VALUES(?,?,?,?,?,?,?,?)`, r.Name, r.SourceID, r.TargetID, r.Profile, r.LocalPort, r.TargetHost, r.TargetPort, boolInt(r.Enabled))
	if err != nil {
		return r, err
	}
	r.ID, _ = res.LastInsertId()
	return r, nil
}

func (s *Store) UpdateRule(ctx context.Context, id int64, r ForwardRule) error {
	r.Profile = NormalizeProfile(r.Profile)
	res, err := s.db.ExecContext(ctx, `UPDATE forward_rules SET name=?,source_id=?,target_id=?,profile=?,local_port=?,target_host=?,target_port=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		r.Name, r.SourceID, r.TargetID, r.Profile, r.LocalPort, r.TargetHost, r.TargetPort, boolInt(r.Enabled), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM forward_rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) StartSession(ctx context.Context, sourceID, targetID, profile, path string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO sessions(source_id,target_id,profile,path) VALUES(?,?,?,?)`, sourceID, targetID, NormalizeProfile(profile), path)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) TouchSession(ctx context.Context, id int64, relayBytes int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen=CURRENT_TIMESTAMP, relay_bytes=relay_bytes+? WHERE id=?`, relayBytes, id)
	return err
}

func (s *Store) UpdateSessionPathForPair(ctx context.Context, aID, bID, profile, path string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions
		SET path=?, last_seen=CURRENT_TIMESTAMP
		WHERE id=(
			SELECT id FROM sessions
			WHERE ended_at='' AND profile=? AND ((source_id=? AND target_id=?) OR (source_id=? AND target_id=?))
			ORDER BY id DESC
			LIMIT 1
		)`, path, NormalizeProfile(profile), aID, bID, bID, aID)
	return err
}

func (s *Store) EndIdleSessions(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET ended_at=CURRENT_TIMESTAMP WHERE ended_at='' AND last_seen < ?`, cutoff.Format(time.RFC3339))
	return err
}

func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,source_id,target_id,profile,path,relay_bytes,started_at,last_seen,ended_at FROM sessions ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var x Session
		if err := rows.Scan(&x.ID, &x.SourceID, &x.TargetID, &x.Profile, &x.Path, &x.RelayBytes, &x.StartedAt, &x.LastSeen, &x.EndedAt); err != nil {
			return nil, err
		}
		x.Profile = NormalizeProfile(x.Profile)
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) Metrics(ctx context.Context) (Metrics, error) {
	var m Metrics
	queries := []struct {
		q string
		v *int
	}{
		{`SELECT COUNT(*) FROM devices`, &m.Devices},
		{`SELECT COUNT(*) FROM devices WHERE online=1`, &m.OnlineDevices},
		{`SELECT COUNT(*) FROM forward_rules`, &m.ForwardRules},
		{`SELECT COUNT(*) FROM sessions WHERE ended_at=''`, &m.ActiveSessions},
	}
	for _, q := range queries {
		if err := s.db.QueryRowContext(ctx, q.q).Scan(q.v); err != nil {
			return m, err
		}
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(relay_bytes),0) FROM sessions`).Scan(&m.RelayBytes); err != nil {
		return m, err
	}
	return m, nil
}

func (s *Store) PutTunnelState(ctx context.Context, ts TunnelState) error {
	ts.Profile = NormalizeProfile(ts.Profile)
	lastTransitionAt := ts.LastTransitionAt
	if lastTransitionAt == "" {
		lastTransitionAt = time.Now().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tunnel_states(device_id,peer_id,profile,state,via,nat_type,public_addr,conv_id,rtt_ms,last_error,attempt,next_retry_at,last_transition_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(device_id,peer_id,profile) DO UPDATE SET state=excluded.state, via=excluded.via,
			nat_type=excluded.nat_type, public_addr=excluded.public_addr, conv_id=excluded.conv_id, rtt_ms=excluded.rtt_ms,
			last_error=excluded.last_error, attempt=excluded.attempt, next_retry_at=excluded.next_retry_at,
			last_transition_at=excluded.last_transition_at, updated_at=CURRENT_TIMESTAMP`,
		ts.DeviceID, ts.PeerID, ts.Profile, ts.State, ts.Via, ts.NATType, ts.PublicAddr, ts.ConvID, ts.RTTMs, ts.LastError, ts.Attempt, ts.NextRetryAt, lastTransitionAt)
	return err
}

func (s *Store) ListTunnelStates(ctx context.Context) ([]TunnelState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,peer_id,profile,state,via,nat_type,public_addr,conv_id,rtt_ms,last_error,attempt,next_retry_at,last_transition_at,updated_at FROM tunnel_states ORDER BY updated_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TunnelState
	for rows.Next() {
		var t TunnelState
		if err := rows.Scan(&t.DeviceID, &t.PeerID, &t.Profile, &t.State, &t.Via, &t.NATType, &t.PublicAddr, &t.ConvID, &t.RTTMs, &t.LastError, &t.Attempt, &t.NextRetryAt, &t.LastTransitionAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Profile = NormalizeProfile(t.Profile)
		out = append(out, t)
	}
	return out, rows.Err()
}

// LocalPortConflict 检查同一 source_id 上是否已经有 enabled 规则用了这个 local_port。
// excludeID > 0 时表示 update 场景，排除自己。
func (s *Store) LocalPortConflict(ctx context.Context, sourceID string, localPort int, excludeID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM forward_rules WHERE source_id=? AND local_port=? AND enabled=1 AND id<>?`,
		sourceID, localPort, excludeID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Store) Audit(ctx context.Context, kind, detail string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(kind,detail) VALUES(?,?)`, kind, detail)
	return err
}

func scanRules(rows *sql.Rows) ([]ForwardRule, error) {
	var out []ForwardRule
	for rows.Next() {
		var r ForwardRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.Name, &r.SourceID, &r.TargetID, &r.Profile, &r.LocalPort, &r.TargetHost, &r.TargetPort, &enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Profile = NormalizeProfile(r.Profile)
		r.Enabled = enabled != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

const (
	ProfileInteractive = "interactive"
	ProfileBulk        = "bulk"
)

func NormalizeProfile(profile string) string {
	profile = strings.TrimSpace(strings.ToLower(profile))
	if profile == "" {
		return ProfileInteractive
	}
	return profile
}

func ValidProfile(profile string) bool {
	switch NormalizeProfile(profile) {
	case ProfileInteractive, ProfileBulk:
		return true
	default:
		return false
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (r ForwardRule) Validate() error {
	r.Profile = NormalizeProfile(r.Profile)
	if !ValidProfile(r.Profile) {
		return errors.New("profile must be interactive or bulk")
	}
	if r.SourceID == "" || r.TargetID == "" {
		return errors.New("source_id and target_id are required")
	}
	if r.SourceID == r.TargetID {
		return errors.New("source_id and target_id must differ")
	}
	if r.LocalPort <= 0 || r.LocalPort >= 65536 || r.TargetPort <= 0 || r.TargetPort >= 65536 {
		return fmt.Errorf("ports must be 1..65535")
	}
	if r.TargetHost == "" {
		return errors.New("target_host is required")
	}
	return nil
}
