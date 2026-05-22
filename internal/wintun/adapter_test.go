package wintun

import "testing"

func TestNormalizeConfigDefaults(t *testing.T) {
	cfg := normalizeConfig(Config{})
	if cfg.Name != DefaultAdapterName {
		t.Fatalf("name=%q", cfg.Name)
	}
	if cfg.MTU != DefaultMTU {
		t.Fatalf("mtu=%d", cfg.MTU)
	}
}
