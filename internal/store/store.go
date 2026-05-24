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
	ID                  string `json:"id"`
	Username            string `json:"username"`
	Name                string `json:"name"`
	Role                string `json:"role"`
	ForcePasswordChange bool   `json:"force_password_change"`
	PasswordVersion     int64  `json:"password_version"`
	PasswordHash        string `json:"-"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

type VirtualNetwork struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CIDR      string `json:"cidr"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type VirtualAddress struct {
	DeviceID   string `json:"device_id"`
	NetworkID  int64  `json:"network_id"`
	VirtualIP  string `json:"virtual_ip"`
	Hostname   string `json:"hostname"`
	DNSEnabled bool   `json:"dns_enabled"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type VirtualDeviceKey struct {
	DeviceID  string `json:"device_id"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"public_key"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type VirtualDeviceGroup struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type VirtualDeviceGroupMember struct {
	GroupID   string `json:"group_id"`
	DeviceID  string `json:"device_id"`
	CreatedAt string `json:"created_at"`
}

type VirtualACLRule struct {
	ID             int64  `json:"id"`
	NetworkID      int64  `json:"network_id"`
	SourceDeviceID string `json:"source_device_id"`
	SourceGroupID  string `json:"source_group_id"`
	TargetDeviceID string `json:"target_device_id"`
	TargetGroupID  string `json:"target_group_id"`
	Protocol       string `json:"protocol"`
	PortStart      int    `json:"port_start"`
	PortEnd        int    `json:"port_end"`
	Action         string `json:"action"`
	Enabled        bool   `json:"enabled"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type VirtualRoute struct {
	ID        int64  `json:"id"`
	DeviceID  string `json:"device_id"`
	NetworkID int64  `json:"network_id"`
	CIDR      string `json:"cidr"`
	Advertise bool   `json:"advertise"`
	Accept    bool   `json:"accept"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type VirtualPeerState struct {
	DeviceID         string `json:"device_id"`
	PeerID           string `json:"peer_id"`
	NetworkID        int64  `json:"network_id"`
	State            string `json:"state"`
	Path             string `json:"path"`
	AdapterState     string `json:"adapter_state"`
	RouteConflict    string `json:"route_conflict"`
	SelectedCIDR     string `json:"selected_cidr"`
	MTU              int    `json:"mtu"`
	MSS              int    `json:"mss"`
	RTTMs            int    `json:"rtt_ms"`
	TxBytes          int64  `json:"tx_bytes"`
	RxBytes          int64  `json:"rx_bytes"`
	DropReason       string `json:"drop_reason"`
	LastError        string `json:"last_error"`
	LastHandshakeAt  string `json:"last_handshake_at"`
	LastTransitionAt string `json:"last_transition_at"`
	UpdatedAt        string `json:"updated_at"`
}

const (
	ProfileInteractive = "interactive"
	ProfileBulk        = "bulk"
	ProfileLANPacket   = "lan-packet"
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
