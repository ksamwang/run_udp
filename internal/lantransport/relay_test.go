package lantransport

import "testing"

func TestRelayFrameRoundTrip(t *testing.T) {
	packed, err := PackRelayFrame(RelayFrame{SrcDevice: "dev-a", DstDevice: "dev-b", Payload: []byte("ciphertext")})
	if err != nil {
		t.Fatal(err)
	}
	if !IsRelayFrame(packed) {
		t.Fatal("packed relay frame must be detected")
	}
	got, err := UnpackRelayFrame(packed)
	if err != nil {
		t.Fatal(err)
	}
	if got.SrcDevice != "dev-a" || got.DstDevice != "dev-b" || string(got.Payload) != "ciphertext" {
		t.Fatalf("bad relay frame: %+v", got)
	}
}

func TestRelayFrameViewAvoidsPayloadCopy(t *testing.T) {
	packed, err := PackRelayFrame(RelayFrame{SrcDevice: "dev-a", DstDevice: "dev-b", Payload: []byte("ciphertext")})
	if err != nil {
		t.Fatal(err)
	}
	view, err := UnpackRelayFrameView(packed)
	if err != nil {
		t.Fatal(err)
	}
	view.Payload[0] = 'C'
	if packed[len(packed)-len(view.Payload)] != 'C' {
		t.Fatal("view payload should share the relay frame buffer")
	}
	copied, err := UnpackRelayFrame(packed)
	if err != nil {
		t.Fatal(err)
	}
	copied.Payload[0] = 'x'
	if packed[len(packed)-len(copied.Payload)] == 'x' {
		t.Fatal("copying unpacker must not expose the relay frame buffer")
	}
}

func TestRelayFrameRejectsBadInput(t *testing.T) {
	if _, err := PackRelayFrame(RelayFrame{SrcDevice: "dev-a", DstDevice: "dev-b"}); err == nil {
		t.Fatal("empty payload must be rejected")
	}
	if _, err := UnpackRelayFrame([]byte("bad")); err == nil {
		t.Fatal("bad frame must be rejected")
	}
}

func BenchmarkRelayFramePackUnpack1KB(b *testing.B) {
	frame := RelayFrame{SrcDevice: "dev-a", DstDevice: "dev-b", Payload: make([]byte, 1024)}
	b.SetBytes(int64(len(frame.Payload)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		packed, err := PackRelayFrame(frame)
		if err != nil {
			b.Fatal(err)
		}
		decoded, err := UnpackRelayFrame(packed)
		if err != nil {
			b.Fatal(err)
		}
		if decoded.SrcDevice != frame.SrcDevice || decoded.DstDevice != frame.DstDevice || len(decoded.Payload) != len(frame.Payload) {
			b.Fatal("bad relay frame round trip")
		}
	}
}

func BenchmarkRelayFramePackUnpackView1KB(b *testing.B) {
	frame := RelayFrame{SrcDevice: "dev-a", DstDevice: "dev-b", Payload: make([]byte, 1024)}
	b.SetBytes(int64(len(frame.Payload)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		packed, err := PackRelayFrame(frame)
		if err != nil {
			b.Fatal(err)
		}
		decoded, err := UnpackRelayFrameView(packed)
		if err != nil {
			b.Fatal(err)
		}
		if decoded.SrcDevice != frame.SrcDevice || decoded.DstDevice != frame.DstDevice || len(decoded.Payload) != len(frame.Payload) {
			b.Fatal("bad relay frame view round trip")
		}
	}
}
