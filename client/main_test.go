package main

import (
	"testing"
	"time"
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
