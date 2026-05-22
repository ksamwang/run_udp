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
	ID           string `gorm:"primaryKey;size:191;column:id"`
	Username     string `gorm:"uniqueIndex;size:191;not null;column:username"`
	Name         string `gorm:"size:255;not null;default:'';column:name"`
	Role         string `gorm:"size:64;not null;default:'admin';column:role"`
	PasswordHash string `gorm:"size:191;not null;column:password_hash"`
	CreatedAt    string `gorm:"size:64;not null;column:created_at"`
	UpdatedAt    string `gorm:"size:64;not null;column:updated_at"`
}

func (AdminUser) TableName() string { return "admin_users" }
