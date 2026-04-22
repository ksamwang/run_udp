package config

import (
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
	if cfg.Server != "1.2.3.4:7000" || cfg.DeviceID != "A" {
		t.Fatalf("config not loaded: %+v", cfg)
	}
	if cfg.UPnPTimeout != 2*time.Second || cfg.PunchTimeout != 7*time.Second {
		t.Fatalf("durations not parsed: %+v", cfg)
	}
}

func TestLoadMissingConfigIsAllowed(t *testing.T) {
	cfg := DefaultServer()
	if err := LoadJSON(filepath.Join(t.TempDir(), "missing.json"), &cfg); err != nil {
		t.Fatal(err)
	}
}

func TestServerAllowRelayDefaultsToTrueWhenOmitted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(path, []byte(`{"psk":"secret","allow_legacy":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultServer()
	if err := LoadJSON(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowRelay {
		t.Fatal("allow_relay should remain true when omitted")
	}
	if !cfg.AllowLegacy {
		t.Fatal("allow_legacy should be loaded")
	}
}
