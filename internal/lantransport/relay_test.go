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

func TestRelayFrameRejectsBadInput(t *testing.T) {
	if _, err := PackRelayFrame(RelayFrame{SrcDevice: "dev-a", DstDevice: "dev-b"}); err == nil {
		t.Fatal("empty payload must be rejected")
	}
	if _, err := UnpackRelayFrame([]byte("bad")); err == nil {
		t.Fatal("bad frame must be rejected")
	}
}
