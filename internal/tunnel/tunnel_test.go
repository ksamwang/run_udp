package tunnel

import (
	"testing"

	"udp_tunnel_demo/internal/store"
)

func TestProfileConfig(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		want    ProfileConfig
	}{
		{
			name:    "interactive default",
			profile: store.ProfileInteractive,
			want:    ProfileConfig{KCPWindow: 1024},
		},
		{
			name:    "bulk",
			profile: store.ProfileBulk,
			want:    ProfileConfig{KCPWindow: 8192, UDPSocketBuffer: 16 * 1024 * 1024},
		},
		{
			name:    "lan packet",
			profile: store.ProfileLANPacket,
			want:    ProfileConfig{KCPWindow: 8192, UDPSocketBuffer: 16 * 1024 * 1024},
		},
		{
			name:    "unknown falls back to interactive",
			profile: "unknown",
			want:    ProfileConfig{KCPWindow: 1024},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := profileConfig(tt.profile); got != tt.want {
				t.Fatalf("profileConfig(%q)=%+v, want %+v", tt.profile, got, tt.want)
			}
		})
	}
}
