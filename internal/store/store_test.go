package store

import "testing"

func TestForwardRuleValidation(t *testing.T) {
	valid := ForwardRule{
		Name: "rdp", SourceID: "A", TargetID: "B", Profile: ProfileInteractive,
		LocalPort: 13389, TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := valid
	bad.Profile = "video"
	if err := bad.Validate(); err == nil {
		t.Fatal("expected invalid profile")
	}
	bad = valid
	bad.TargetID = "A"
	if err := bad.Validate(); err == nil {
		t.Fatal("expected same device validation error")
	}
	bad = valid
	bad.TargetPort = 70000
	if err := bad.Validate(); err == nil {
		t.Fatal("expected port validation error")
	}
}

func TestNormalizeProfile(t *testing.T) {
	if NormalizeProfile("") != ProfileInteractive {
		t.Fatal("empty profile should default to interactive")
	}
	if NormalizeProfile(" BULK ") != ProfileBulk {
		t.Fatal("profile should be normalized")
	}
}
