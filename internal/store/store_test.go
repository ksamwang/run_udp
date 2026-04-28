package store

import (
	"context"
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
	id, err := s.StartSession(ctx, "A", "B", "pending")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected session id")
	}
	if err := s.UpdateSessionPathForPair(ctx, "B", "A", "p2p"); err != nil {
		t.Fatal(err)
	}
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Path != "p2p" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
	if err := s.PutTunnelState(ctx, TunnelState{DeviceID: "A", PeerID: "B", State: "p2p", Via: "p2p", NATType: "cone", ConvID: 123, RTTMs: 45, LastError: "none"}); err != nil {
		t.Fatal(err)
	}
	states, err := s.ListTunnelStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].RTTMs != 45 || states[0].ConvID != 123 || states[0].NATType != "cone" || states[0].LastError != "none" {
		t.Fatalf("unexpected tunnel states: %+v", states)
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
