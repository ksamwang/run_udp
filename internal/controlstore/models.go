package controlstore

type Meta struct {
	Key   string `gorm:"primaryKey;size:191;column:key"`
	Value string `gorm:"type:text;not null;column:value"`
}

func (Meta) TableName() string { return "meta" }

type SystemSetting struct {
	Key       string `gorm:"primaryKey;size:191;column:key"`
	Value     string `gorm:"type:text;not null;column:value"`
	UpdatedAt string `gorm:"size:64;not null;column:updated_at"`
}

func (SystemSetting) TableName() string { return "system_settings" }

type Device struct {
	ID        string `gorm:"primaryKey;size:191;column:id"`
	Name      string `gorm:"size:255;not null;default:'';column:name"`
	Addr      string `gorm:"size:255;not null;default:'';column:addr"`
	UpnpAddr  string `gorm:"size:255;not null;default:'';column:upnp_addr"`
	Want      string `gorm:"size:191;not null;default:'';column:want"`
	Online    bool   `gorm:"index;not null;default:false;column:online"`
	Enabled   bool   `gorm:"index;not null;default:true;column:enabled"`
	LastSeen  string `gorm:"size:64;not null;default:'';column:last_seen"`
	CreatedAt string `gorm:"size:64;not null;column:created_at"`
}

func (Device) TableName() string { return "devices" }

type ForwardRule struct {
	ID         int64  `gorm:"primaryKey;column:id"`
	Name       string `gorm:"size:255;not null;default:'';column:name"`
	SourceID   string `gorm:"index;size:191;not null;column:source_id"`
	TargetID   string `gorm:"index;size:191;not null;column:target_id"`
	Profile    string `gorm:"size:32;not null;default:'interactive';column:profile"`
	LocalPort  int    `gorm:"not null;column:local_port"`
	TargetHost string `gorm:"size:255;not null;column:target_host"`
	TargetPort int    `gorm:"not null;column:target_port"`
	Enabled    bool   `gorm:"index;not null;default:true;column:enabled"`
	CreatedAt  string `gorm:"size:64;not null;column:created_at"`
	UpdatedAt  string `gorm:"size:64;not null;column:updated_at"`
}

func (ForwardRule) TableName() string { return "forward_rules" }

type Session struct {
	ID         int64  `gorm:"primaryKey;column:id"`
	SourceID   string `gorm:"index;size:191;not null;column:source_id"`
	TargetID   string `gorm:"index;size:191;not null;column:target_id"`
	Profile    string `gorm:"size:32;not null;default:'interactive';column:profile"`
	Path       string `gorm:"size:32;not null;column:path"`
	RelayBytes int64  `gorm:"not null;default:0;column:relay_bytes"`
	StartedAt  string `gorm:"size:64;not null;column:started_at"`
	LastSeen   string `gorm:"index;size:64;not null;column:last_seen"`
	EndedAt    string `gorm:"index;size:64;not null;default:'';column:ended_at"`
}

func (Session) TableName() string { return "sessions" }

type AuditEvent struct {
	ID        int64  `gorm:"primaryKey;column:id"`
	Kind      string `gorm:"size:64;not null;column:kind"`
	Detail    string `gorm:"type:text;not null;column:detail"`
	CreatedAt string `gorm:"size:64;not null;column:created_at"`
}

func (AuditEvent) TableName() string { return "audit_events" }

type TunnelState struct {
	DeviceID         string `gorm:"primaryKey;size:191;column:device_id"`
	PeerID           string `gorm:"primaryKey;size:191;column:peer_id"`
	Profile          string `gorm:"primaryKey;size:32;column:profile"`
	State            string `gorm:"size:32;not null;default:'';column:state"`
	Via              string `gorm:"size:32;not null;default:'';column:via"`
	NATType          string `gorm:"size:64;not null;default:'';column:nat_type"`
	PublicAddr       string `gorm:"size:255;not null;default:'';column:public_addr"`
	ConvID           int64  `gorm:"not null;default:0;column:conv_id"`
	RTTMs            int    `gorm:"not null;default:0;column:rtt_ms"`
	LastError        string `gorm:"type:text;not null;column:last_error"`
	Attempt          int    `gorm:"not null;default:0;column:attempt"`
	NextRetryAt      string `gorm:"size:64;not null;default:'';column:next_retry_at"`
	LastTransitionAt string `gorm:"size:64;not null;default:'';column:last_transition_at"`
	UpdatedAt        string `gorm:"index;size:64;not null;column:updated_at"`
}

func (TunnelState) TableName() string { return "tunnel_states" }

type AdminRefreshToken struct {
	ID         int64  `gorm:"primaryKey;column:id"`
	UserID     string `gorm:"index;size:191;not null;column:user_id"`
	TokenHash  string `gorm:"uniqueIndex;size:191;not null;column:token_hash"`
	ExpiresAt  string `gorm:"size:64;not null;column:expires_at"`
	RevokedAt  string `gorm:"size:64;not null;default:'';column:revoked_at"`
	CreatedAt  string `gorm:"size:64;not null;column:created_at"`
	LastUsedAt string `gorm:"size:64;not null;default:'';column:last_used_at"`
	UserAgent  string `gorm:"size:512;not null;default:'';column:user_agent"`
	IP         string `gorm:"size:64;not null;default:'';column:ip"`
}

func (AdminRefreshToken) TableName() string { return "admin_refresh_tokens" }

type AdminUser struct {
	ID                  string `gorm:"primaryKey;size:191;column:id"`
	Username            string `gorm:"uniqueIndex;size:191;not null;column:username"`
	Name                string `gorm:"size:255;not null;default:'';column:name"`
	Role                string `gorm:"size:64;not null;default:'admin';column:role"`
	ForcePasswordChange bool   `gorm:"not null;default:true;column:force_password_change"`
	PasswordVersion     int64  `gorm:"not null;default:1;column:password_version"`
	PasswordHash        string `gorm:"size:191;not null;column:password_hash"`
	CreatedAt           string `gorm:"size:64;not null;column:created_at"`
	UpdatedAt           string `gorm:"size:64;not null;column:updated_at"`
}

func (AdminUser) TableName() string { return "admin_users" }

type VirtualNetwork struct {
	ID        int64  `gorm:"primaryKey;column:id"`
	Name      string `gorm:"size:191;not null;column:name"`
	CIDR      string `gorm:"uniqueIndex;size:64;not null;column:cidr"`
	Enabled   bool   `gorm:"index;not null;default:true;column:enabled"`
	CreatedAt string `gorm:"size:64;not null;column:created_at"`
	UpdatedAt string `gorm:"size:64;not null;column:updated_at"`
}

func (VirtualNetwork) TableName() string { return "virtual_networks" }

type VirtualAddress struct {
	DeviceID   string  `gorm:"primaryKey;size:191;column:device_id"`
	NetworkID  int64   `gorm:"primaryKey;uniqueIndex:idx_virtual_addresses_ip,priority:1;uniqueIndex:idx_virtual_addresses_hostname,priority:1;column:network_id"`
	VirtualIP  string  `gorm:"uniqueIndex:idx_virtual_addresses_ip,priority:2;size:64;not null;column:virtual_ip"`
	Hostname   *string `gorm:"uniqueIndex:idx_virtual_addresses_hostname,priority:2;size:191;column:hostname"`
	DNSEnabled bool    `gorm:"not null;default:false;column:dns_enabled"`
	CreatedAt  string  `gorm:"size:64;not null;column:created_at"`
	UpdatedAt  string  `gorm:"size:64;not null;column:updated_at"`
}

func (VirtualAddress) TableName() string { return "virtual_addresses" }

type VirtualDeviceKey struct {
	DeviceID  string `gorm:"primaryKey;size:191;column:device_id"`
	Algorithm string `gorm:"size:32;not null;column:algorithm"`
	PublicKey string `gorm:"type:text;not null;column:public_key"`
	CreatedAt string `gorm:"size:64;not null;column:created_at"`
	UpdatedAt string `gorm:"size:64;not null;column:updated_at"`
}

func (VirtualDeviceKey) TableName() string { return "virtual_device_keys" }

type VirtualDeviceGroup struct {
	ID        string `gorm:"primaryKey;size:191;column:id"`
	Name      string `gorm:"size:191;not null;column:name"`
	CreatedAt string `gorm:"size:64;not null;column:created_at"`
	UpdatedAt string `gorm:"size:64;not null;column:updated_at"`
}

func (VirtualDeviceGroup) TableName() string { return "virtual_device_groups" }

type VirtualDeviceGroupMember struct {
	GroupID   string `gorm:"primaryKey;size:191;column:group_id"`
	DeviceID  string `gorm:"primaryKey;size:191;column:device_id"`
	CreatedAt string `gorm:"size:64;not null;column:created_at"`
}

func (VirtualDeviceGroupMember) TableName() string { return "virtual_device_group_members" }

type VirtualACLRule struct {
	ID             int64  `gorm:"primaryKey;column:id"`
	NetworkID      int64  `gorm:"index;not null;column:network_id"`
	SourceDeviceID string `gorm:"index;size:191;not null;default:'';column:source_device_id"`
	SourceGroupID  string `gorm:"index;size:191;not null;default:'';column:source_group_id"`
	TargetDeviceID string `gorm:"index;size:191;not null;default:'';column:target_device_id"`
	TargetGroupID  string `gorm:"index;size:191;not null;default:'';column:target_group_id"`
	Protocol       string `gorm:"size:16;not null;default:'tcp';column:protocol"`
	PortStart      int    `gorm:"not null;default:0;column:port_start"`
	PortEnd        int    `gorm:"not null;default:0;column:port_end"`
	Action         string `gorm:"size:16;not null;default:'allow';column:action"`
	Enabled        bool   `gorm:"index;not null;default:true;column:enabled"`
	CreatedAt      string `gorm:"size:64;not null;column:created_at"`
	UpdatedAt      string `gorm:"size:64;not null;column:updated_at"`
}

func (VirtualACLRule) TableName() string { return "virtual_acl_rules" }

type VirtualRoute struct {
	ID        int64  `gorm:"primaryKey;column:id"`
	DeviceID  string `gorm:"uniqueIndex:idx_virtual_routes_device_network_cidr,priority:1;index;size:191;not null;column:device_id"`
	NetworkID int64  `gorm:"uniqueIndex:idx_virtual_routes_device_network_cidr,priority:2;index;not null;column:network_id"`
	CIDR      string `gorm:"uniqueIndex:idx_virtual_routes_device_network_cidr,priority:3;size:64;not null;column:cidr"`
	Advertise bool   `gorm:"not null;default:false;column:advertise"`
	Accept    bool   `gorm:"not null;default:true;column:accept"`
	CreatedAt string `gorm:"size:64;not null;column:created_at"`
	UpdatedAt string `gorm:"size:64;not null;column:updated_at"`
}

func (VirtualRoute) TableName() string { return "virtual_routes" }

type VirtualPeerState struct {
	DeviceID         string `gorm:"primaryKey;size:191;column:device_id"`
	PeerID           string `gorm:"primaryKey;size:191;column:peer_id"`
	NetworkID        int64  `gorm:"primaryKey;column:network_id"`
	State            string `gorm:"size:32;not null;default:'';column:state"`
	Path             string `gorm:"size:32;not null;default:'';column:path"`
	AdapterState     string `gorm:"size:32;not null;default:'';column:adapter_state"`
	RouteConflict    string `gorm:"type:text;not null;column:route_conflict"`
	SelectedCIDR     string `gorm:"size:64;not null;default:'';column:selected_cidr"`
	MTU              int    `gorm:"not null;default:0;column:mtu"`
	MSS              int    `gorm:"not null;default:0;column:mss"`
	RTTMs            int    `gorm:"not null;default:0;column:rtt_ms"`
	TxBytes          int64  `gorm:"not null;default:0;column:tx_bytes"`
	RxBytes          int64  `gorm:"not null;default:0;column:rx_bytes"`
	DropReason       string `gorm:"size:64;not null;default:'';column:drop_reason"`
	LastError        string `gorm:"type:text;not null;column:last_error"`
	LastHandshakeAt  string `gorm:"size:64;not null;default:'';column:last_handshake_at"`
	LastTransitionAt string `gorm:"size:64;not null;default:'';column:last_transition_at"`
	UpdatedAt        string `gorm:"index;size:64;not null;column:updated_at"`
}

func (VirtualPeerState) TableName() string { return "virtual_peer_states" }
