package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadClientConfigDurations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(path, []byte(`{"server":"1.2.3.4:7000","device_id":"A","upnp_timeout":"2s","punch_timeout":"7s"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultClient()
	if err := LoadJSON(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "1.2.3.4:7000" || cfg.DeviceID != "" {
		t.Fatalf("config not loaded: %+v", cfg)
	}
	if cfg.UPnPTimeout != 2*time.Second || cfg.PunchTimeout != 7*time.Second {
		t.Fatalf("durations not parsed: %+v", cfg)
	}
}

func TestLoadMissingConfigIsAllowed(t *testing.T) {
	cfg := DefaultClient()
	if err := LoadJSON(filepath.Join(t.TempDir(), "missing.json"), &cfg); err != nil {
		t.Fatal(err)
	}
}

func TestLoadServerEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(`
UDP_LISTEN=:17000
CONTROL_DATABASE_DSN='user:pass@tcp(127.0.0.1:3306)/udp_tunnel'
ADMIN_ACCESS_TOKEN_TTL=2h
ALLOW_RELAY=false
CLIENT_TRAY_ENABLED=false
CLIENT_LOG_LEVEL=debug
`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultServer()
	if err := LoadServerEnv(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.UDPListen != ":17000" || cfg.ControlDatabaseDSN != "user:pass@tcp(127.0.0.1:3306)/udp_tunnel" {
		t.Fatalf("env strings not loaded: %+v", cfg)
	}
	if cfg.AdminAccessTokenTTL != 2*time.Hour {
		t.Fatalf("duration not loaded: %+v", cfg.AdminAccessTokenTTL)
	}
	if !cfg.AllowRelay || !cfg.ClientTrayEnabled || cfg.ClientLogLevel != "info" {
		t.Fatalf("database-owned settings should not be loaded from env: %+v", cfg)
	}
}

func TestSaveClientLocalJSONWritesMinimalBootstrapConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	cfg := DefaultClient()
	cfg.Server = "1.2.3.4:7000"
	cfg.ServerHTTP = "http://tunnel.example.com"
	cfg.DeviceID = "dev-1234"
	cfg.DeviceName = "office-pc"
	cfg.PSK = "secret"
	cfg.NoUPnP = true
	if err := SaveClientLocalJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" {
		t.Fatal("expected config content")
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["server_http"] != "http://tunnel.example.com" || got["device_name"] != "office-pc" || got["psk"] != "secret" {
		t.Fatalf("unexpected saved config: %v", got)
	}
	if _, ok := got["device_id"]; ok {
		t.Fatalf("device_id should not be persisted: %v", got)
	}
	if _, ok := got["server"]; ok {
		t.Fatalf("server should not be persisted: %v", got)
	}
	if _, ok := got["no_upnp"]; ok {
		t.Fatalf("runtime field should not be persisted: %v", got)
	}
}
