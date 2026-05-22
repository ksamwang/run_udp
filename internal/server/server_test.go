package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestAdminJWTLoginRefreshAndMe(t *testing.T) {
	a := newTestApp(t)
	pass := "secret-pass"
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.PutMeta(context.Background(), "admin_password_hash", string(hash)); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, a.httpMux(), http.MethodPost, "/api/admin/auth/login", map[string]any{"password": pass}, nil)
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
	db, err := store.Open(filepath.Join(t.TempDir(), "server-test.db"))
	if err != nil {
		t.Fatal(err)
	}
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
	token, err := a.signAccessToken(adminClaims{
		Subject: adminUserID,
		Role:    "admin",
		Issued:  time.Now().Unix(),
		Expires: time.Now().Add(time.Hour).Unix(),
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
