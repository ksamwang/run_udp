//go:build !windows

package wintun

import "testing"

func TestOpenOrCreateUnsupported(t *testing.T) {
	if _, err := OpenOrCreate(Config{}); err != ErrUnsupported {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}
