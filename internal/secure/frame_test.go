package secure

import "testing"

func TestFrameRoundTrip(t *testing.T) {
	c, err := NewCodec("secret")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Seal(KindControl, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !IsFrame(b) {
		t.Fatal("sealed packet is not detected as frame")
	}
	kind, plain, err := c.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindControl || string(plain) != "hello" {
		t.Fatalf("unexpected open result kind=%d plain=%q", kind, plain)
	}
}

func TestConvIDStableAndOrdered(t *testing.T) {
	a := ConvID("secret", "A", "B")
	b := ConvID("secret", "B", "A")
	if a == 0 || a != b {
		t.Fatalf("conv id should be non-zero and order independent: %d %d", a, b)
	}
	if a == ConvID("other", "A", "B") {
		t.Fatal("conv id should depend on psk")
	}
}
