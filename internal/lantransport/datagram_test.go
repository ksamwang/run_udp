package lantransport

import (
	"errors"
	"fmt"
	"testing"

	"udp_tunnel_demo/internal/packet"
)

func TestDatagramEncryptDecryptOutOfOrderAndReplay(t *testing.T) {
	keys, err := packet.DeriveSessionKeys([]byte("shared-secret"), 7, "session", "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := packet.NewCodec(keys.AB, 7, packet.TypeIPv4)
	if err != nil {
		t.Fatal(err)
	}
	rx, err := packet.NewCodec(keys.AB, 7, packet.TypeIPv4)
	if err != nil {
		t.Fatal(err)
	}

	first, err := Seal(tx, []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Seal(tx, []byte("second"))
	if err != nil {
		t.Fatal(err)
	}
	if !IsFrame(first) || string(first) == "first" {
		t.Fatalf("datagram must be framed and encrypted: %q", string(first))
	}
	if got, err := Open(rx, second); err != nil || string(got) != "second" {
		t.Fatalf("out-of-order second open failed: got=%q err=%v", got, err)
	}
	if got, err := Open(rx, first); err != nil || string(got) != "first" {
		t.Fatalf("out-of-order first open failed: got=%q err=%v", got, err)
	}
	if _, err := Open(rx, first); !errors.Is(err, packet.ErrReplay) {
		t.Fatalf("expected replay rejection, got %v", err)
	}
}

func TestDatagramOpenAfterLoss(t *testing.T) {
	keys, err := packet.DeriveSessionKeys([]byte("shared-secret"), 7, "session", "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := packet.NewCodec(keys.BA, 7, packet.TypeIPv4)
	if err != nil {
		t.Fatal(err)
	}
	rx, err := packet.NewCodec(keys.BA, 7, packet.TypeIPv4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Seal(tx, []byte("dropped")); err != nil {
		t.Fatal(err)
	}
	delivered, err := Seal(tx, []byte("delivered"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(rx, delivered)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "delivered" {
		t.Fatalf("bad delivered payload: %q", got)
	}
}

func BenchmarkDatagramSealOpen1KB(b *testing.B) {
	keys, err := packet.DeriveSessionKeys([]byte("shared-secret"), 7, "session", "dev-a", "dev-b")
	if err != nil {
		b.Fatal(err)
	}
	tx, err := packet.NewCodec(keys.AB, 7, packet.TypeIPv4)
	if err != nil {
		b.Fatal(err)
	}
	rx, err := packet.NewCodec(keys.AB, 7, packet.TypeIPv4)
	if err != nil {
		b.Fatal(err)
	}
	payload := make([]byte, 1024)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		frame, err := Seal(tx, payload)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := Open(rx, frame); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDatagramSealOpenSizes(b *testing.B) {
	for _, size := range []int{1200, 1280, 1400} {
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			keys, err := packet.DeriveSessionKeys([]byte("shared-secret"), 7, "session", "dev-a", "dev-b")
			if err != nil {
				b.Fatal(err)
			}
			tx, err := packet.NewCodec(keys.AB, 7, packet.TypeIPv4)
			if err != nil {
				b.Fatal(err)
			}
			rx, err := packet.NewCodec(keys.AB, 7, packet.TypeIPv4)
			if err != nil {
				b.Fatal(err)
			}
			payload := make([]byte, size)
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				frame, err := Seal(tx, payload)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := Open(rx, frame); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
