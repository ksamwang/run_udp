package lan

import (
	"path/filepath"
	"testing"
)

func TestLoadAndSaveConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lan.json")
	if err := SaveConfig(path, Config{ServerHTTP: " http://api.example.com ", LogLevel: " info "}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerHTTP != "http://api.example.com" || cfg.LogLevel != "info" {
		t.Fatalf("bad config: %+v", cfg)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != (Config{}) {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}
