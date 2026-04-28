package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"udp_tunnel_demo/internal/config"
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
			rec := doWebJSON(t, a.httpMux(), http.MethodPost, "/api/forwards", tc.body)
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

	rec := doWebJSON(t, a.httpMux(), http.MethodPost, "/api/forwards", map[string]any{
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

	rec := doWebJSON(t, a.httpMux(), http.MethodDelete, "/api/devices/dev-a", nil)
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

	rec := doWebJSON(t, a.httpMux(), http.MethodPatch, "/api/devices/dev-b", map[string]any{"enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doWebJSON(t, a.httpMux(), http.MethodGet, "/api/devices", nil)
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

func newTestApp(t *testing.T) *app {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "server-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultServer()
	cfg.PSK = "test-psk"
	return &app{
		cfg:       cfg,
		db:        db,
		startTime: time.Now(),
		peers:     map[string]map[string]*peer{},
		pairByID:  map[string]int64{},
		sessions:  map[string]time.Time{"test-session": time.Now().Add(time.Hour)},
	}
}

func mustUpsertDevice(t *testing.T, a *app, ctx context.Context, id string, enabled bool) {
	t.Helper()
	if err := a.db.UpsertDevice(ctx, id, id, "", "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetDeviceEnabled(ctx, id, enabled); err != nil {
		t.Fatal(err)
	}
}

func doWebJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := newJSONRequest(t, method, path, body)
	req.AddCookie(&http.Cookie{Name: "udp_tunnel_session", Value: "test-session"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
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
