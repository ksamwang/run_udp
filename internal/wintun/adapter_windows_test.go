//go:build windows

package wintun

import "testing"

func TestCIDRMask(t *testing.T) {
	tests := map[string]string{
		"172.16.10.0/24": "255.255.255.0",
		"10.8.0.0/16":    "255.255.0.0",
		"10.8.0.0/32":    "255.255.255.255",
	}
	for cidr, want := range tests {
		got, err := cidrMask(cidr)
		if err != nil {
			t.Fatalf("cidrMask(%q): %v", cidr, err)
		}
		if got != want {
			t.Fatalf("cidrMask(%q)=%q, want %q", cidr, got, want)
		}
	}
}

func TestDefaultRingSizeSupportsHighThroughput(t *testing.T) {
	if DefaultRingSize < 16*1024*1024 {
		t.Fatalf("default ring size too small: %d", DefaultRingSize)
	}
}
