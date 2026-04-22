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
	if err := s.UpsertDevice(ctx, "A", "1.1.1.1:1", "", "B", true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDevice(ctx, "B", "2.2.2.2:2", "", "A", true); err != nil {
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
	if err := s.UpsertDevice(ctx, "A", "1.1.1.1:1", "", "", true); err != nil {
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
