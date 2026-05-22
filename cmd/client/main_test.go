package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if _, ok := got["device_id"]; ok {
		t.Fatalf("device_id should not be exposed in local config UI: %+v", got)
	}
	if _, ok := got["psk"]; ok {
		t.Fatalf("psk should not be exposed in local config UI: %+v", got)
	}
	if got["server_http"] != cfg.ServerHTTP || got["device_name"] != cfg.DeviceName {
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
	body := bytes.NewBufferString(`{"server_http":"http://new.example.com","device_name":"","psk":"ignored-secret","no_upnp":true,"log_level":"debug"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/config", body)
	req.Header.Set("Content-Type", "application/json")
	state.handleConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if saved.ServerHTTP != "http://new.example.com" || saved.DeviceName == "" {
		t.Fatalf("bootstrap fields not saved: %+v", saved)
	}
	if saved.Server != "" || saved.PeerID != "" || saved.PSK != "" || saved.NoUPnP || saved.UPnPTimeout != 0 || saved.LogLevel != "" || saved.TrayEnabled || saved.PunchTimeout != 0 || saved.ForceRelay || saved.AllowLegacy || len(saved.Forwards) != 0 {
		t.Fatalf("server-managed fields should be cleared before save: %+v", saved)
	}
}

func TestNeedsBootstrapConfigDoesNotRequireLocalPSK(t *testing.T) {
	cfg := config.DefaultClient()
	cfg.ServerHTTP = "http://tunnel.example.com"
	cfg.PSK = ""
	if needsBootstrapConfig(cfg, false, false) {
		t.Fatal("local psk should not be required for bootstrap config")
	}
}

func TestMergeBootstrapAppliesServerPSK(t *testing.T) {
	local := config.DefaultClient()
	local.ServerHTTP = "http://old.example.com"
	local.PSK = ""
	merged, err := mergeBootstrap(local, bootstrapResponse{
		DeviceID:     "DEV-1234",
		Server:       "1.2.3.4:7000",
		ServerHTTP:   "http://tunnel.example.com",
		PSK:          "server-secret",
		UPnPTimeout:  "3s",
		PunchTimeout: "15s",
		LogLevel:     "debug",
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged.PSK != "server-secret" || merged.Server != "1.2.3.4:7000" || merged.ServerHTTP != "http://tunnel.example.com" {
		t.Fatalf("bootstrap fields not merged: %+v", merged)
	}
}

func TestAgentBootstrapDoesNotSendLocalPSK(t *testing.T) {
	cfg := config.DefaultClient()
	cfg.ServerHTTP = "http://tunnel.example.com"
	cfg.DeviceID = "DEV-1234"
	cfg.PSK = "local-secret"

	var gotHeader string
	var gotPath string
	oldDo := doAgentHTTPRequest
	doAgentHTTPRequest = func(req *http.Request) (*http.Response, error) {
		gotHeader = req.Header.Get("X-UDP-Tunnel-PSK")
		gotPath = req.URL.Path
		rec := httptest.NewRecorder()
		_ = json.NewEncoder(rec).Encode(bootstrapResponse{PSK: "server-secret"})
		return rec.Result(), nil
	}
	defer func() { doAgentHTTPRequest = oldDo }()

	resp, err := agentBootstrap(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/agent/bootstrap" || gotHeader != "" || resp.PSK != "server-secret" {
		t.Fatalf("unexpected bootstrap request path=%q header=%q resp=%+v", gotPath, gotHeader, resp)
	}
}

func TestAgentPostSendsBootstrapPSK(t *testing.T) {
	cfg := config.DefaultClient()
	cfg.ServerHTTP = "http://tunnel.example.com"
	cfg.PSK = "server-secret"

	var gotHeader string
	oldDo := doAgentHTTPRequest
	doAgentHTTPRequest = func(req *http.Request) (*http.Response, error) {
		gotHeader = req.Header.Get("X-UDP-Tunnel-PSK")
		return httptest.NewRecorder().Result(), nil
	}
	defer func() { doAgentHTTPRequest = oldDo }()

	if err := agentPost(cfg, "/api/agent/register", map[string]any{"device_id": "DEV-1234"}); err != nil {
		t.Fatal(err)
	}
	if gotHeader != "server-secret" {
		t.Fatalf("agent post should send bootstrap psk, got %q", gotHeader)
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
	if len(first) != len("DEV-")+16 || first[:4] != "DEV-" || first != strings.ToUpper(first) {
		t.Fatalf("unexpected id format: %q", first)
	}
}
