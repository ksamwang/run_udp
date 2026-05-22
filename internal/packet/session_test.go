package packet

import (
	"errors"
	"testing"
)

func TestPacketSessionRoundTrip(t *testing.T) {
	keys, err := DeriveSessionKeys([]byte("shared-secret"), 7, "session-1", "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := NewCodec(keys.AB, 7, TypeIPv4)
	if err != nil {
		t.Fatal(err)
	}
	rx, err := NewCodec(keys.AB, 7, TypeIPv4)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte{0x45, 0x00, 0x00, 0x14}
	frame, err := tx.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := rx.Open(frame)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("plain mismatch: %x != %x", got, plain)
	}
}

func TestPacketSessionRejectsReplay(t *testing.T) {
	keys, err := DeriveSessionKeys([]byte("shared-secret"), 7, "session-1", "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := NewCodec(keys.AB, 7, TypeIPv4)
	rx, _ := NewCodec(keys.AB, 7, TypeIPv4)
	frame, err := tx.Seal([]byte("packet"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rx.Open(frame); err != nil {
		t.Fatal(err)
	}
	if _, err := rx.Open(frame); !errors.Is(err, ErrReplay) {
		t.Fatalf("expected replay error, got %v", err)
	}
}

func TestPacketSessionRejectsTamperAndWrongNetwork(t *testing.T) {
	keys, err := DeriveSessionKeys([]byte("shared-secret"), 7, "session-1", "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := NewCodec(keys.AB, 7, TypeIPv4)
	rx, _ := NewCodec(keys.AB, 7, TypeIPv4)
	frame, err := tx.Seal([]byte("packet"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), frame...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := rx.Open(tampered); !errors.Is(err, ErrFrame) {
		t.Fatalf("expected frame error, got %v", err)
	}
	wrongNetwork, _ := NewCodec(keys.AB, 8, TypeIPv4)
	if _, err := wrongNetwork.Open(frame); !errors.Is(err, ErrFrame) {
		t.Fatalf("expected wrong network frame error, got %v", err)
	}
}

func TestDeriveSessionKeysDirectionalAndStable(t *testing.T) {
	first, err := DeriveSessionKeys([]byte("shared-secret"), 7, "session-1", "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	second, err := DeriveSessionKeys([]byte("shared-secret"), 7, "session-1", "dev-b", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if first.AB != second.AB || first.BA != second.BA {
		t.Fatal("ordered device salt should keep derivation stable")
	}
	if first.AB == first.BA {
		t.Fatal("directional keys must differ")
	}
}
