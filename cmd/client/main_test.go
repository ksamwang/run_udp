package main

import (
	"testing"
	"time"

	"udp_tunnel_demo/internal/store"
)

func TestShouldRetryTunnelError(t *testing.T) {
	tests := []struct {
		reason string
		want   bool
	}{
		{reason: "rule_cancelled", want: false},
		{reason: "rule_changed", want: false},
		{reason: "ingress_listen_failed", want: false},
		{reason: "register_failed", want: true},
		{reason: "kcp_eof", want: true},
	}
	for _, tc := range tests {
		if got := shouldRetryTunnelError(tc.reason); got != tc.want {
			t.Fatalf("reason=%s want=%v got=%v", tc.reason, tc.want, got)
		}
	}
}

func TestNextBackoffDelayRangeAndCap(t *testing.T) {
	cases := []struct {
		attempt int
		base    time.Duration
	}{
		{attempt: 1, base: 1 * time.Second},
		{attempt: 2, base: 2 * time.Second},
		{attempt: 3, base: 5 * time.Second},
		{attempt: 4, base: 10 * time.Second},
		{attempt: 5, base: 20 * time.Second},
		{attempt: 6, base: 30 * time.Second},
		{attempt: 7, base: 60 * time.Second},
		{attempt: 99, base: 60 * time.Second},
	}
	for _, tc := range cases {
		got := nextBackoffDelay(tc.attempt)
		min := tc.base + time.Duration(float64(tc.base)*0.10)
		max := tc.base + time.Duration(float64(tc.base)*0.20)
		if got < min || got > max {
			t.Fatalf("attempt=%d base=%s got=%s expected in [%s,%s]", tc.attempt, tc.base, got, min, max)
		}
	}
}

func TestGroupRulesByPeerSplitsProfiles(t *testing.T) {
	rules := []store.ForwardRule{
		{ID: 1, SourceID: "A", TargetID: "B", Profile: store.ProfileInteractive, LocalPort: 13389, TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true},
		{ID: 2, SourceID: "A", TargetID: "B", Profile: store.ProfileBulk, LocalPort: 1445, TargetHost: "127.0.0.1", TargetPort: 445, Enabled: true},
	}
	grouped := groupRulesByPeer("A", rules)
	if len(grouped) != 2 {
		t.Fatalf("expected two profile groups, got %+v", grouped)
	}
	if got := grouped[tunnelGroupKey("B", store.ProfileInteractive)]; got.Profile != store.ProfileInteractive || len(got.Forward) != 1 {
		t.Fatalf("bad interactive group: %+v", got)
	}
	if got := grouped[tunnelGroupKey("B", store.ProfileBulk)]; got.Profile != store.ProfileBulk || len(got.Forward) != 1 {
		t.Fatalf("bad bulk group: %+v", got)
	}
}

func TestSmuxConfigProfiles(t *testing.T) {
	interactive := smuxConfig(store.ProfileInteractive)
	bulk := smuxConfig(store.ProfileBulk)
	if interactive.MaxStreamBuffer != 512*1024 || interactive.MaxReceiveBuffer != 8*1024*1024 {
		t.Fatalf("unexpected interactive config: %+v", interactive)
	}
	if bulk.MaxStreamBuffer != 16*1024*1024 || bulk.MaxReceiveBuffer != 64*1024*1024 {
		t.Fatalf("unexpected bulk config: %+v", bulk)
	}
}
