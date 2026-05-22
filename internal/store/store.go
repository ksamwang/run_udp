package store

import (
	"errors"
	"fmt"
	"strings"
)

type Device struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Addr          string `json:"addr"`
	UpnpAddr      string `json:"upnp_addr,omitempty"`
	Want          string `json:"want,omitempty"`
	Online        bool   `json:"online"`
	Enabled       bool   `json:"enabled"`
	LastSeen      string `json:"last_seen"`
	CreatedAt     string `json:"created_at"`
	HealthSummary string `json:"health_summary,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

type ForwardRule struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	SourceID      string `json:"source_id"`
	TargetID      string `json:"target_id"`
	Profile       string `json:"profile"`
	LocalPort     int    `json:"local_port"`
	TargetHost    string `json:"target_host"`
	TargetPort    int    `json:"target_port"`
	Enabled       bool   `json:"enabled"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	RuntimeState  string `json:"runtime_state,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	LastUpdatedAt string `json:"last_updated_at,omitempty"`
	Attempt       int    `json:"attempt,omitempty"`
	NextRetryAt   string `json:"next_retry_at,omitempty"`
}

type Session struct {
	ID         int64  `json:"id"`
	SourceID   string `json:"source_id"`
	TargetID   string `json:"target_id"`
	Profile    string `json:"profile"`
	Path       string `json:"path"`
	RelayBytes int64  `json:"relay_bytes"`
	StartedAt  string `json:"started_at"`
	LastSeen   string `json:"last_seen"`
	EndedAt    string `json:"ended_at,omitempty"`
}

type Metrics struct {
	Devices        int   `json:"devices"`
	OnlineDevices  int   `json:"online_devices"`
	ForwardRules   int   `json:"forward_rules"`
	ActiveSessions int   `json:"active_sessions"`
	RelayBytes     int64 `json:"relay_bytes"`
}

type TunnelState struct {
	DeviceID         string `json:"device_id"`
	PeerID           string `json:"peer_id"`
	Profile          string `json:"profile"`
	State            string `json:"state"`
	Via              string `json:"via"`
	NATType          string `json:"nat_type"`
	PublicAddr       string `json:"public_addr"`
	ConvID           int64  `json:"conv_id"`
	RTTMs            int    `json:"rtt_ms"`
	LastError        string `json:"last_error"`
	Attempt          int    `json:"attempt"`
	NextRetryAt      string `json:"next_retry_at"`
	LastTransitionAt string `json:"last_transition_at"`
	UpdatedAt        string `json:"updated_at"`
}

type AdminRefreshToken struct {
	ID         int64  `json:"id"`
	UserID     string `json:"user_id"`
	TokenHash  string `json:"-"`
	ExpiresAt  string `json:"expires_at"`
	RevokedAt  string `json:"revoked_at,omitempty"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
	IP         string `json:"ip,omitempty"`
}

type AdminUser struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

const (
	ProfileInteractive = "interactive"
	ProfileBulk        = "bulk"
)

func NormalizeProfile(profile string) string {
	profile = strings.TrimSpace(strings.ToLower(profile))
	if profile == "" {
		return ProfileInteractive
	}
	return profile
}

func ValidProfile(profile string) bool {
	switch NormalizeProfile(profile) {
	case ProfileInteractive, ProfileBulk:
		return true
	default:
		return false
	}
}

func (r ForwardRule) Validate() error {
	r.Profile = NormalizeProfile(r.Profile)
	if !ValidProfile(r.Profile) {
		return errors.New("profile must be interactive or bulk")
	}
	if r.SourceID == "" || r.TargetID == "" {
		return errors.New("source_id and target_id are required")
	}
	if r.SourceID == r.TargetID {
		return errors.New("source_id and target_id must differ")
	}
	if r.LocalPort <= 0 || r.LocalPort >= 65536 || r.TargetPort <= 0 || r.TargetPort >= 65536 {
		return fmt.Errorf("ports must be 1..65535")
	}
	if r.TargetHost == "" {
		return errors.New("target_host is required")
	}
	return nil
}
