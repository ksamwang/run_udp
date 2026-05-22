package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"udp_tunnel_demo/internal/config"
	"udp_tunnel_demo/internal/protocol"
	"udp_tunnel_demo/internal/store"
)

func TestHandleForwardsValidationErrors(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", false)

	tests := []struct {
		name string
		body map[string]any
		code string
	}{
		{
			name: "missing source device",
			body: map[string]any{"name": "rdp", "source_id": "missing", "target_id": "dev-a", "local_port": 11388, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true},
			code: "device_not_found",
		},
		{
			name: "disabled target device",
			body: map[string]any{"name": "rdp", "source_id": "dev-a", "target_id": "dev-b", "local_port": 11388, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true},
			code: "device_disabled",
		},
		{
			name: "same device forbidden",
			body: map[string]any{"name": "rdp", "source_id": "dev-a", "target_id": "dev-a", "local_port": 11388, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true},
			code: "same_device_forbidden",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/rules", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp["code"] != tc.code {
				t.Fatalf("expected code %q got %v body=%s", tc.code, resp["code"], rec.Body.String())
			}
		})
	}
}

func TestHandleForwardsLocalPortConflict(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	mustUpsertDevice(t, a, ctx, "dev-c", true)
	if _, err := a.db.CreateRule(ctx, store.ForwardRule{
		Name: "rdp-1", SourceID: "dev-a", TargetID: "dev-b", LocalPort: 11388,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/rules", map[string]any{
		"name": "rdp-2", "source_id": "dev-a", "target_id": "dev-c", "local_port": 11388,
		"target_host": "127.0.0.1", "target_port": 3389, "enabled": true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "local_port_conflict" {
		t.Fatalf("expected local_port_conflict got %v body=%s", resp["code"], rec.Body.String())
	}
}

func TestHandleForwardsProfileValidationAndBulkRule(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)

	rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/rules", map[string]any{
		"name": "smb", "source_id": "dev-a", "target_id": "dev-b", "profile": "bulk", "local_port": 1445,
		"target_host": "127.0.0.1", "target_port": 445, "enabled": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rule store.ForwardRule
	if err := json.Unmarshal(rec.Body.Bytes(), &rule); err != nil {
		t.Fatal(err)
	}
	if rule.Profile != store.ProfileBulk {
		t.Fatalf("expected bulk profile: %+v", rule)
	}

	rec = doAdminJSON(t, a, http.MethodPost, "/api/admin/rules", map[string]any{
		"name": "bad", "source_id": "dev-a", "target_id": "dev-b", "profile": "video", "local_port": 1446,
		"target_host": "127.0.0.1", "target_port": 445, "enabled": true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEnrichedRulesMatchesProfileTunnelState(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	if _, err := a.db.CreateRule(ctx, store.ForwardRule{
		Name: "rdp", SourceID: "dev-a", TargetID: "dev-b", Profile: store.ProfileInteractive, LocalPort: 13389,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.CreateRule(ctx, store.ForwardRule{
		Name: "smb", SourceID: "dev-a", TargetID: "dev-b", Profile: store.ProfileBulk, LocalPort: 1445,
		TargetHost: "127.0.0.1", TargetPort: 445, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutTunnelState(ctx, store.TunnelState{
		DeviceID: "dev-a", PeerID: "dev-b", Profile: store.ProfileBulk, State: "p2p", Via: "p2p",
	}); err != nil {
		t.Fatal(err)
	}
	rules, err := a.enrichedRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stateByName := map[string]string{}
	for _, r := range rules {
		stateByName[r.Name] = r.RuntimeState
	}
	if stateByName["smb"] != "p2p" || stateByName["rdp"] != "down" {
		t.Fatalf("unexpected runtime states: %+v", stateByName)
	}
}

func TestRegisterPairsSameDevicesByProfile(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	a.handleRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 11001}, &protocol.Message{From: "dev-a", Peer: "dev-b", Profile: store.ProfileInteractive})
	a.handleRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 11002}, &protocol.Message{From: "dev-b", Peer: "dev-a", Profile: store.ProfileInteractive})
	a.handleRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12001}, &protocol.Message{From: "dev-a", Peer: "dev-b", Profile: store.ProfileBulk})
	a.handleRegister(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12002}, &protocol.Message{From: "dev-b", Peer: "dev-a", Profile: store.ProfileBulk})
	if len(a.pairByID) != 2 {
		t.Fatalf("expected separate profile pair sessions, got %+v", a.pairByID)
	}
	if _, ok := a.peers["dev-a"][peerSlotKey("dev-b", store.ProfileInteractive)]; !ok {
		t.Fatalf("missing interactive peer slot: %+v", a.peers)
	}
	if _, ok := a.peers["dev-a"][peerSlotKey("dev-b", store.ProfileBulk)]; !ok {
		t.Fatalf("missing bulk peer slot: %+v", a.peers)
	}
}

func TestHandleDeviceDeleteBlockedByEnabledRule(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	if _, err := a.db.CreateRule(ctx, store.ForwardRule{
		Name: "rdp", SourceID: "dev-a", TargetID: "dev-b", LocalPort: 11388,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodDelete, "/api/admin/devices/dev-a", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "device_in_use" {
		t.Fatalf("expected device_in_use got %v body=%s", resp["code"], rec.Body.String())
	}
}

func TestHandleDevicePatchDisablesAndListShowsHealth(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)
	if _, err := a.db.CreateRule(ctx, store.ForwardRule{
		Name: "rdp", SourceID: "dev-a", TargetID: "dev-b", LocalPort: 11388,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutTunnelState(ctx, store.TunnelState{
		DeviceID: "dev-a", PeerID: "dev-b", State: "p2p", Via: "p2p", LastError: "",
	}); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodPatch, "/api/admin/devices/dev-b", map[string]any{"enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doAdminJSON(t, a, http.MethodGet, "/api/admin/devices", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var devices []store.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("unexpected devices: %+v", devices)
	}
	var got store.Device
	for _, d := range devices {
		if d.ID == "dev-b" {
			got = d
			break
		}
	}
	if got.Enabled {
		t.Fatalf("expected device disabled: %+v", got)
	}
	if got.HealthSummary != "至少一条隧道正常" {
		t.Fatalf("expected health summary from paired tunnel, got %+v", got)
	}
}

func TestHandleAgentEndpointsRejectDisabledDevice(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", false)

	rec := doAgentJSON(t, a.httpMux(), http.MethodPost, "/api/agent/register", map[string]any{
		"device_id": "dev-a",
		"name":      "dev-a",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "device_disabled" {
		t.Fatalf("expected device_disabled got %v body=%s", resp["code"], rec.Body.String())
	}

	rec = doAgentJSON(t, a.httpMux(), http.MethodGet, "/api/agent/rules?device_id=dev-a", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	resp = map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "device_disabled" {
		t.Fatalf("expected device_disabled got %v body=%s", resp["code"], rec.Body.String())
	}
}

func TestHandleAgentTunnelStatusDoesNotOverwriteNameWithID(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.db.UpsertDevice(ctx, "dev-a", "office-pc", "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetDeviceEnabled(ctx, "dev-a", true); err != nil {
		t.Fatal(err)
	}
	mustUpsertDevice(t, a, ctx, "dev-b", true)

	rec := doAgentJSON(t, a.httpMux(), http.MethodPost, "/api/agent/tunnel-status", map[string]any{
		"device_id": "dev-a",
		"peer":      "dev-b",
		"profile":   "interactive",
		"state":     "p2p",
		"via":       "p2p",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err := a.db.GetDevice(ctx, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "office-pc" {
		t.Fatalf("tunnel status should not overwrite display name with id: %+v", d)
	}
}

func TestAgentRegisterAndHeartbeatEmptyNamePreserveDisplayName(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.db.UpsertDevice(ctx, "dev-a", "office-pc", "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetDeviceEnabled(ctx, "dev-a", true); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/agent/register", "/api/agent/heartbeat"} {
		rec := doAgentJSON(t, a.httpMux(), http.MethodPost, path, map[string]any{
			"device_id": "dev-a",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	d, err := a.db.GetDevice(ctx, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "office-pc" {
		t.Fatalf("empty agent names should preserve display name: %+v", d)
	}
}

func TestHandleClientRelease(t *testing.T) {
	a := newTestApp(t)
	a.cfg.ClientReleaseVersion = "0.4.0"
	a.cfg.ClientReleaseSHA256 = "abc123"
	a.cfg.ClientReleasePublishedAt = "2026-04-28T18:00:00Z"
	a.cfg.ClientReleaseNotes = "stable release"
	a.cfg.ClientReleaseMinimumSupported = "0.3.0"
	a.cfg.ClientReleaseURL = "https://example.com/udp-tunnel-client-0.4.0-setup.exe"

	rec := doAgentJSON(t, a.httpMux(), http.MethodGet, "/api/client/release", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp clientReleaseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Version != "0.4.0" || resp.URL == "" || resp.SHA256 != "abc123" {
		t.Fatalf("unexpected release response: %+v", resp)
	}
}

func TestApplyStoredSettingsUsesSystemSettings(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.db.PutSystemSetting(ctx, settingPeerTTL, "45s"); err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutSystemSetting(ctx, settingAllowRelay, "false"); err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutSystemSetting(ctx, settingClientLogLevel, "debug"); err != nil {
		t.Fatal(err)
	}

	if err := a.applyStoredSettings(); err != nil {
		t.Fatal(err)
	}

	if a.cfg.PeerTTL != 45*time.Second || a.cfg.AllowRelay || a.cfg.ClientLogLevel != "debug" {
		t.Fatalf("settings not applied from database: %+v", a.cfg)
	}
	if got, err := a.db.GetSystemSetting(ctx, settingPairTTL); err != nil || got != config.DefaultServer().PairTTL.String() {
		t.Fatalf("default setting not persisted: got=%q err=%v", got, err)
	}
}

func TestApplyStoredSettingsMigratesLegacyMeta(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.db.PutMeta(ctx, "setting_peer_ttl", "55s"); err != nil {
		t.Fatal(err)
	}

	if err := a.applyStoredSettings(); err != nil {
		t.Fatal(err)
	}

	if a.cfg.PeerTTL != 55*time.Second {
		t.Fatalf("legacy meta setting was not applied: %+v", a.cfg)
	}
	got, err := a.db.GetSystemSetting(ctx, settingPeerTTL)
	if err != nil {
		t.Fatal(err)
	}
	if got != "55s" {
		t.Fatalf("legacy setting was not migrated, got %q", got)
	}
}

func TestHandleSettingsPersistsSystemSettings(t *testing.T) {
	a := newTestApp(t)
	rec := doAdminJSON(t, a, http.MethodPatch, "/api/admin/settings", map[string]any{
		"peer_ttl":                                 "45s",
		"pair_ttl":                                 "1m",
		"relay_idle_timeout":                       "2m",
		"allow_relay":                              false,
		"allow_legacy":                             false,
		"client_no_upnp":                           true,
		"client_upnp_timeout":                      "2s",
		"client_log_level":                         "debug",
		"client_tray_enabled":                      false,
		"client_punch_timeout":                     "12s",
		"client_force_relay":                       true,
		"client_allow_legacy":                      false,
		"client_release_version":                   "1.2.3",
		"client_release_url":                       "https://example.com/client.exe",
		"client_release_sha256":                    "abc123",
		"client_release_published_at":              "2026-05-22T12:00:00Z",
		"client_release_notes":                     "release notes",
		"client_release_minimum_supported_version": "1.0.0",
		"client_release_file":                      "client.exe",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got, err := a.db.GetSystemSetting(context.Background(), settingClientLogLevel)
	if err != nil {
		t.Fatal(err)
	}
	if got != "debug" {
		t.Fatalf("expected setting in system settings table, got %q", got)
	}
	legacy, err := a.db.GetMeta(context.Background(), "setting_client_log_level")
	if err != nil {
		t.Fatal(err)
	}
	if legacy != "" {
		t.Fatalf("settings should not be persisted to meta, got %q", legacy)
	}
}

func TestAdminJWTLoginRefreshAndMe(t *testing.T) {
	a := newTestApp(t)
	pass := "secret-pass"
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertAdminUser(context.Background(), store.AdminUser{
		ID: "admin", Username: "admin", Name: "Administrator", Role: "admin", PasswordHash: string(hash),
	}); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{"username": "admin", "password": pass}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	var loginResp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatal(err)
	}
	if loginResp.AccessToken == "" || loginResp.RefreshToken == "" {
		t.Fatalf("missing tokens: %+v", loginResp)
	}

	rec = doJSON(t, a.httpMux(), http.MethodGet, "/api/admin/me", nil, map[string]string{"Authorization": "Bearer " + loginResp.AccessToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/refresh", map[string]any{"refresh_token": loginResp.RefreshToken}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", rec.Code, rec.Body.String())
	}
	var refreshResp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &refreshResp); err != nil {
		t.Fatal(err)
	}
	if refreshResp.AccessToken == "" || refreshResp.RefreshToken == "" || refreshResp.RefreshToken == loginResp.RefreshToken {
		t.Fatalf("bad refresh response: %+v", refreshResp)
	}
}

func TestEnsureAdminPasswordCreatesDefaultAdmin(t *testing.T) {
	a := newTestApp(t)
	if err := a.ensureAdminUser(); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{"username": defaultAdminUsername, "password": defaultAdminPassword}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	var loginResp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatal(err)
	}
	if !loginResp.ForcePasswordChange {
		t.Fatalf("expected default admin to require password change: %+v", loginResp)
	}
}

func TestEnsureAdminUserMigratesLegacyPasswordHash(t *testing.T) {
	a := newTestApp(t)
	pass := "legacy-pass"
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutMeta(context.Background(), "admin_password_hash", string(hash)); err != nil {
		t.Fatal(err)
	}
	if err := a.ensureAdminUser(); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{"username": defaultAdminUsername, "password": pass}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChangePasswordClearsForceFlag(t *testing.T) {
	a := newTestApp(t)
	pass := "secret-pass"
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertAdminUser(context.Background(), store.AdminUser{
		ID: "admin", Username: "admin", Name: "Administrator", Role: "admin", ForcePasswordChange: true, PasswordVersion: 1, PasswordHash: string(hash),
	}); err != nil {
		t.Fatal(err)
	}
	loginRec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{"username": "admin", "password": pass}, nil)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp tokenResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/password", map[string]any{
		"current_password": pass,
		"new_password":     "new-secret-pass",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password status=%d body=%s", rec.Code, rec.Body.String())
	}
	user, err := a.db.GetAdminUserByID(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if user.ForcePasswordChange {
		t.Fatalf("force flag not cleared: %+v", user)
	}

	rec = doJSON(t, a.httpMux(), http.MethodGet, "/api/admin/me", nil, map[string]string{"Authorization": "Bearer " + loginResp.AccessToken})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old access token should be invalidated after password change, got=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChangePasswordRevokesExistingRefreshTokens(t *testing.T) {
	a := newTestApp(t)
	pass := "secret-pass"
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.UpsertAdminUser(context.Background(), store.AdminUser{
		ID: "admin", Username: "admin", Name: "Administrator", Role: "admin", ForcePasswordChange: true, PasswordVersion: 1, PasswordHash: string(hash),
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.CreateAdminRefreshToken(context.Background(), "admin", "old-refresh", time.Now().Add(time.Hour), "ua", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	rec := doAdminJSON(t, a, http.MethodPost, "/api/admin/password", map[string]any{
		"current_password": pass,
		"new_password":     "new-secret-pass",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password status=%d body=%s", rec.Code, rec.Body.String())
	}
	token, err := a.db.GetAdminRefreshToken(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if token.RevokedAt == "" {
		t.Fatalf("refresh token not revoked after password change: %+v", token)
	}
}

func TestAdminConsoleAPISmoke(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mustUpsertDevice(t, a, ctx, "dev-a", true)
	mustUpsertDevice(t, a, ctx, "dev-b", true)

	rec := doAdminJSON(t, a, http.MethodGet, "/api/admin/devices", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("devices status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doAdminJSON(t, a, http.MethodPost, "/api/admin/rules", map[string]any{
		"name": "rdp", "source_id": "dev-a", "target_id": "dev-b", "profile": "interactive",
		"local_port": 13389, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create rule status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rule store.ForwardRule
	if err := json.Unmarshal(rec.Body.Bytes(), &rule); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/admin/rules", nil},
		{http.MethodGet, "/api/admin/sessions", nil},
		{http.MethodGet, "/api/admin/tunnel-states", nil},
		{http.MethodGet, "/api/admin/metrics", nil},
		{http.MethodGet, "/api/admin/settings", nil},
		{http.MethodPatch, "/api/admin/rules/" + strconv.FormatInt(rule.ID, 10), map[string]any{
			"name": "rdp", "source_id": "dev-a", "target_id": "dev-b", "profile": "interactive",
			"local_port": 13390, "target_host": "127.0.0.1", "target_port": 3389, "enabled": true,
		}},
		{http.MethodPatch, "/api/admin/devices/dev-b", map[string]any{"enabled": false}},
	}
	for _, check := range checks {
		rec = doAdminJSON(t, a, check.method, check.path, check.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", check.method, check.path, rec.Code, rec.Body.String())
		}
	}
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	db := newFakeStore()
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultServer()
	cfg.PSK = "test-psk"
	return &App{
		cfg:       cfg,
		db:        db,
		startTime: time.Now(),
		peers:     map[string]map[string]*peer{},
		pairByID:  map[string]int64{},
	}
}

func mustUpsertDevice(t *testing.T, a *App, ctx context.Context, id string, enabled bool) {
	t.Helper()
	if err := a.db.UpsertDevice(ctx, id, id, "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetDeviceEnabled(ctx, id, enabled); err != nil {
		t.Fatal(err)
	}
}

func doAdminJSON(t *testing.T, a *App, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	if _, err := a.db.GetAdminUserByID(context.Background(), defaultAdminUsername); err != nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.db.UpsertAdminUser(context.Background(), store.AdminUser{
			ID: defaultAdminUsername, Username: defaultAdminUsername, Name: "Administrator", Role: "admin", PasswordVersion: 1, PasswordHash: string(hash),
		}); err != nil {
			t.Fatal(err)
		}
	}
	token, err := a.signAccessToken(adminClaims{
		Subject:         defaultAdminUsername,
		Role:            "admin",
		PasswordVersion: 1,
		Issued:          time.Now().Unix(),
		Expires:         time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := newJSONRequest(t, method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.httpMux().ServeHTTP(rec, req)
	return rec
}

func doAgentJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := newJSONRequest(t, method, path, body)
	req.Header.Set("X-UDP-Tunnel-PSK", "test-psk")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := newJSONRequest(t, method, path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newJSONRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	return req
}
