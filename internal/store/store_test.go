package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestRulesAndMetrics(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertDevice(ctx, "A", "A", "1.1.1.1:1", "", "B", true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDevice(ctx, "B", "B", "2.2.2.2:2", "", "A", true); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateRule(ctx, ForwardRule{
		Name: "rdp", SourceID: "A", TargetID: "B", LocalPort: 13389,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := s.RulesForDevice(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].TargetID != "B" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
	if rules[0].Profile != ProfileInteractive {
		t.Fatalf("expected default profile interactive, got %+v", rules[0])
	}
	metrics, err := s.Metrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Devices != 2 || metrics.ForwardRules != 1 {
		t.Fatalf("bad metrics: %+v", metrics)
	}
}

func TestMarkOfflineBefore(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertDevice(ctx, "A", "A", "1.1.1.1:1", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOfflineBefore(ctx, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDevice(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	if d.Online {
		t.Fatal("device should be offline")
	}
}

func TestUpsertDevicePreservesNonEmptyFields(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertDevice(ctx, "A", "A", "1.1.1.1:1", "2.2.2.2:2", "B", true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDevice(ctx, "A", "", "", "", "", true); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDevice(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	if d.Addr != "1.1.1.1:1" || d.UpnpAddr != "2.2.2.2:2" || d.Want != "B" {
		t.Fatalf("fields unexpectedly cleared: %+v", d)
	}
}

func TestUpdateSessionPathForPairAndTunnelState(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	id, err := s.StartSession(ctx, "A", "B", ProfileBulk, "pending")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected session id")
	}
	if err := s.UpdateSessionPathForPair(ctx, "B", "A", ProfileBulk, "p2p"); err != nil {
		t.Fatal(err)
	}
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Path != "p2p" || sessions[0].Profile != ProfileBulk {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
	if err := s.PutTunnelState(ctx, TunnelState{
		DeviceID: "A", PeerID: "B", Profile: ProfileBulk, State: "p2p", Via: "p2p", NATType: "cone",
		ConvID: 123, RTTMs: 45, LastError: "none", Attempt: 2, NextRetryAt: "2026-04-28T03:00:00Z", LastTransitionAt: "2026-04-28T02:59:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	states, err := s.ListTunnelStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Profile != ProfileBulk || states[0].RTTMs != 45 || states[0].ConvID != 123 || states[0].NATType != "cone" || states[0].LastError != "none" || states[0].Attempt != 2 || states[0].NextRetryAt == "" || states[0].LastTransitionAt == "" {
		t.Fatalf("unexpected tunnel states: %+v", states)
	}
}

func TestRuleProfileValidationAndBulkPersistence(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertDevice(ctx, "A", "A", "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDevice(ctx, "B", "B", "", "", "", true); err != nil {
		t.Fatal(err)
	}
	rule := ForwardRule{
		Name: "smb", SourceID: "A", TargetID: "B", Profile: ProfileBulk, LocalPort: 1445,
		TargetHost: "127.0.0.1", TargetPort: 445, Enabled: true,
	}
	if err := rule.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	rules, err := s.RulesForDevice(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Profile != ProfileBulk {
		t.Fatalf("unexpected rules: %+v", rules)
	}
	bad := rule
	bad.Profile = "video"
	if err := bad.Validate(); err == nil {
		t.Fatal("expected invalid profile error")
	}
}

func TestMigrateOldDatabaseAddsInteractiveProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE forward_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			source_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			local_port INTEGER NOT NULL,
			target_host TEXT NOT NULL,
			target_port INTEGER NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`INSERT INTO forward_rules(name,source_id,target_id,local_port,target_host,target_port,enabled) VALUES('rdp','A','B',13389,'127.0.0.1',3389,1);`,
		`CREATE TABLE tunnel_states (
			device_id TEXT NOT NULL,
			peer_id TEXT NOT NULL,
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
			PRIMARY KEY (device_id, peer_id)
		);`,
		`INSERT INTO tunnel_states(device_id,peer_id,state,via) VALUES('A','B','p2p','p2p');`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rules, err := s.ListRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Profile != ProfileInteractive {
		t.Fatalf("unexpected migrated rules: %+v", rules)
	}
	states, err := s.ListTunnelStates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Profile != ProfileInteractive {
		t.Fatalf("unexpected migrated states: %+v", states)
	}
	if err := s.PutTunnelState(context.Background(), TunnelState{DeviceID: "A", PeerID: "B", Profile: ProfileBulk, State: "p2p", Via: "p2p"}); err != nil {
		t.Fatal(err)
	}
	states, err = s.ListTunnelStates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("expected separate profile states after migration, got %+v", states)
	}
}

func TestDeviceEnabledAndRuleReferenceCount(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertDevice(ctx, "A", "A", "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDevice(ctx, "B", "B", "", "", "", true); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateRule(ctx, ForwardRule{
		Name: "rdp", SourceID: "A", TargetID: "B", LocalPort: 11388,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	count, err := s.EnabledRuleReferenceCount(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected enabled rule count 1, got %d", count)
	}
	if err := s.SetDeviceEnabled(ctx, "A", false); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDevice(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	if d.Enabled {
		t.Fatalf("expected device disabled: %+v", d)
	}
}
