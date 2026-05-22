package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"udp_tunnel_demo/internal/config"
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

func TestClientConfigUIOnlyExposesBootstrapFields(t *testing.T) {
	cfg := config.DefaultClient()
	cfg.Server = "1.2.3.4:7000"
	cfg.ServerHTTP = "http://tunnel.example.com"
	cfg.DeviceID = "dev-1234"
	cfg.DeviceName = "office-pc"
	cfg.PSK = "secret"
	cfg.NoUPnP = true
	cfg.LogLevel = "debug"
	cfg.TrayEnabled = true
	cfg.PunchTimeout = 10 * time.Second
	state := &clientConfigState{cfg: cfg}

	rec := httptest.NewRecorder()
	state.handleConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"server", "no_upnp", "log_level", "tray_enabled", "punch_timeout", "force_relay", "forwards"} {
		if _, ok := got[key]; ok {
			t.Fatalf("runtime field %s should not be exposed: %+v", key, got)
		}
	}
	if got["server_http"] != cfg.ServerHTTP || got["device_id"] != cfg.DeviceID || got["device_name"] != cfg.DeviceName || got["psk"] != cfg.PSK {
		t.Fatalf("bootstrap fields missing: %+v", got)
	}
}

func TestClientConfigUIPostClearsServerManagedFields(t *testing.T) {
	cfg := config.DefaultClient()
	cfg.Server = "1.2.3.4:7000"
	cfg.ServerHTTP = "http://old.example.com"
	cfg.DeviceID = "dev-1234"
	cfg.DeviceName = "office-pc"
	cfg.PSK = "old-secret"
	cfg.PeerID = "peer"
	cfg.NoUPnP = true
	cfg.UPnPTimeout = 3 * time.Second
	cfg.LogLevel = "debug"
	cfg.TrayEnabled = true
	cfg.PunchTimeout = 9 * time.Second
	cfg.ForceRelay = true
	cfg.AllowLegacy = true
	cfg.Forwards = []string{"13389:127.0.0.1:3389"}
	var saved config.Client
	state := &clientConfigState{
		cfg: cfg,
		hooks: clientConfigHooks{
			SaveConfig: func(c config.Client) (bool, error) {
				saved = c
				return false, nil
			},
		},
	}
	body := bytes.NewBufferString(`{"server_http":"http://new.example.com","device_name":"","psk":"new-secret","no_upnp":true,"log_level":"debug"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/config", body)
	req.Header.Set("Content-Type", "application/json")
	state.handleConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if saved.ServerHTTP != "http://new.example.com" || saved.PSK != "new-secret" || saved.DeviceName == "" {
		t.Fatalf("bootstrap fields not saved: %+v", saved)
	}
	if saved.Server != "" || saved.PeerID != "" || saved.NoUPnP || saved.UPnPTimeout != 0 || saved.LogLevel != "" || saved.TrayEnabled || saved.PunchTimeout != 0 || saved.ForceRelay || saved.AllowLegacy || len(saved.Forwards) != 0 {
		t.Fatalf("server-managed fields should be cleared before save: %+v", saved)
	}
}

func TestStableDeviceIDFromMachineUUID(t *testing.T) {
	uuid := "4C4C4544-0038-3510-8050-B9C04F4E3332"
	first := stableDeviceID(uuid)
	second := stableDeviceID(uuid)
	other := stableDeviceID("4C4C4544-0038-3510-8050-B9C04F4E3333")
	if first == "" || first != second {
		t.Fatalf("device id should be stable, first=%q second=%q", first, second)
	}
	if first == other {
		t.Fatalf("different UUIDs should produce different ids: %q", first)
	}
	if len(first) != len("dev-")+16 || first[:4] != "dev-" {
		t.Fatalf("unexpected id format: %q", first)
	}
}
