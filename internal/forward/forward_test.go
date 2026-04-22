package forward

import "testing"

func TestParseRule(t *testing.T) {
	r, err := ParseRule("13389:127.0.0.1:3389")
	if err != nil {
		t.Fatal(err)
	}
	if r.LocalPort != 13389 || r.Target != "127.0.0.1:3389" {
		t.Fatalf("bad rule: %+v", r)
	}
}

func TestParseRuleRejectsBadInput(t *testing.T) {
	if _, err := ParseRule("bad"); err == nil {
		t.Fatal("expected error")
	}
}
