package lan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateIdentityValidates(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateIdentity(id); err != nil {
		t.Fatal(err)
	}
	if id.Algorithm != IdentityKeyAlgorithm || id.PrivateKey == "" || id.PublicKey == "" {
		t.Fatalf("bad identity: %+v", id)
	}
}

func TestLoadOrCreateIdentityPersistsNextToConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "lan.json")
	first, err := LoadOrCreateIdentity(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, identityFileName)); err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateIdentity(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicKey != second.PublicKey || first.PrivateKey != second.PrivateKey {
		t.Fatalf("identity should be stable: first=%+v second=%+v", first, second)
	}
}

func TestStableDeviceID(t *testing.T) {
	first := StableDeviceID("550e8400-e29b-41d4-a716-446655440000")
	second := StableDeviceID(" 550E8400-E29B-41D4-A716-446655440000 ")
	third := StableDeviceID("650e8400-e29b-41d4-a716-446655440000")
	if first == "" || first != second {
		t.Fatalf("device id should be stable ignoring case/space: %q %q", first, second)
	}
	if first == third {
		t.Fatalf("different seeds should produce different ids: %q", first)
	}
}
