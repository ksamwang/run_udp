package vnet

import "testing"

func TestDetectConflict(t *testing.T) {
	conflict, err := DetectConflict("172.16.10.0/24", []Route{{CIDR: "172.16.10.0/24", Interface: "vpn"}})
	if err != nil {
		t.Fatal(err)
	}
	if !conflict.Conflicts || conflict.Existing.Interface != "vpn" {
		t.Fatalf("expected conflict: %+v", conflict)
	}
	none, err := DetectConflict("172.16.10.0/24", []Route{{CIDR: "10.0.0.0/8"}})
	if err != nil {
		t.Fatal(err)
	}
	if none.Conflicts {
		t.Fatalf("unexpected conflict: %+v", none)
	}
	defaultRoute, err := DetectConflict("172.16.10.0/24", []Route{{CIDR: "0.0.0.0/0", Interface: "default"}})
	if err != nil {
		t.Fatal(err)
	}
	if defaultRoute.Conflicts {
		t.Fatalf("default route should not conflict: %+v", defaultRoute)
	}
}

func TestNextAvailableCIDR(t *testing.T) {
	got, err := NextAvailableCIDR("172.16.10.0/24", []Route{{CIDR: "172.16.10.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "172.16.11.0/24" {
		t.Fatalf("cidr=%q", got)
	}
}

func TestNextAvailableCIDRIgnoresDefaultRoute(t *testing.T) {
	got, err := NextAvailableCIDR("172.16.10.0/24", []Route{{CIDR: "0.0.0.0/0", Interface: "default"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "172.16.10.0/24" {
		t.Fatalf("cidr=%q", got)
	}
}

func TestMSSForMTU(t *testing.T) {
	if got := MSSForMTU(1280); got != 1200 {
		t.Fatalf("mss=%d", got)
	}
	if got := MSSForMTU(576); got != 536 {
		t.Fatalf("mss min=%d", got)
	}
	if got := MSSForMTU(1500); got != DefaultMSS {
		t.Fatalf("mss clamp=%d", got)
	}
}
