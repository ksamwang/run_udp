package lan

import (
	"testing"
	"time"
)

func TestSignAndVerifyRegister(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Unix()
	sig, err := SignRegister(id, "dev-a", "dev-b", "lan-packet", ts)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRegister(id.PublicKey, "dev-a", "dev-b", "lan-packet", ts, sig); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRegister(id.PublicKey, "dev-a", "dev-c", "lan-packet", ts, sig); err == nil {
		t.Fatal("expected bad signature for changed peer")
	}
}
