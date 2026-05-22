package controlstore

import (
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const (
	DriverMySQL = "mysql"
)

type Config struct {
	Driver      string
	DSN         string
	AutoMigrate bool
}

type Store struct {
	db *gorm.DB
}

type Meta struct {
	Key   string `gorm:"primaryKey;size:191"`
	Value string `gorm:"type:text;not null"`
}

type Device struct {
	ID        string `gorm:"primaryKey;size:191"`
	Name      string `gorm:"size:255;not null;default:''"`
	Addr      string `gorm:"size:255;not null;default:''"`
	UpnpAddr  string `gorm:"size:255;not null;default:''"`
	Want      string `gorm:"size:191;not null;default:''"`
	Online    bool   `gorm:"not null;default:false"`
	Enabled   bool   `gorm:"not null;default:true"`
	LastSeen  string `gorm:"size:64;not null;default:''"`
	CreatedAt time.Time
}

type ForwardRule struct {
	ID         uint   `gorm:"primaryKey"`
	Name       string `gorm:"size:255;not null;default:''"`
	SourceID   string `gorm:"index;size:191;not null"`
	TargetID   string `gorm:"index;size:191;not null"`
	Profile    string `gorm:"size:32;not null;default:'interactive'"`
	LocalPort  int    `gorm:"not null"`
	TargetHost string `gorm:"size:255;not null"`
	TargetPort int    `gorm:"not null"`
	Enabled    bool   `gorm:"index;not null;default:true"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Session struct {
	ID         uint   `gorm:"primaryKey"`
	SourceID   string `gorm:"index;size:191;not null"`
	TargetID   string `gorm:"index;size:191;not null"`
	Profile    string `gorm:"size:32;not null;default:'interactive'"`
	Path       string `gorm:"size:32;not null"`
	RelayBytes int64  `gorm:"not null;default:0"`
	StartedAt  time.Time
	LastSeen   time.Time
	EndedAt    *time.Time
}

type AuditEvent struct {
	ID        uint   `gorm:"primaryKey"`
	Kind      string `gorm:"size:64;not null"`
	Detail    string `gorm:"type:text;not null"`
	CreatedAt time.Time
}

type TunnelState struct {
	DeviceID         string `gorm:"primaryKey;size:191"`
	PeerID           string `gorm:"primaryKey;size:191"`
	Profile          string `gorm:"primaryKey;size:32;default:'interactive'"`
	State            string `gorm:"size:32;not null;default:''"`
	Via              string `gorm:"size:32;not null;default:''"`
	NATType          string `gorm:"size:64;not null;default:''"`
	PublicAddr       string `gorm:"size:255;not null;default:''"`
	ConvID           int64  `gorm:"not null;default:0"`
	RTTMs            int    `gorm:"not null;default:0"`
	LastError        string `gorm:"type:text;not null"`
	Attempt          int    `gorm:"not null;default:0"`
	NextRetryAt      string `gorm:"size:64;not null;default:''"`
	LastTransitionAt string `gorm:"size:64;not null;default:''"`
	UpdatedAt        time.Time
}

type AdminRefreshToken struct {
	ID         uint   `gorm:"primaryKey"`
	UserID     string `gorm:"index;size:191;not null"`
	TokenHash  string `gorm:"uniqueIndex;size:191;not null"`
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
	LastUsedAt *time.Time
	UserAgent  string `gorm:"size:512;not null;default:''"`
	IP         string `gorm:"size:64;not null;default:''"`
}

func Open(cfg Config) (*Store, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = DriverMySQL
	}
	if driver != DriverMySQL {
		return nil, nil
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, nil
	}
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                      cfg.DSN,
		DisableDatetimePrecision: true,
		DontSupportRenameIndex:   true,
		DontSupportRenameColumn:  true,
		DefaultStringSize:        191,
	}), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if cfg.AutoMigrate {
		if err := s.AutoMigrate(); err != nil {
			_ = s.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) AutoMigrate() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.AutoMigrate(
		&Meta{},
		&Device{},
		&ForwardRule{},
		&Session{},
		&AuditEvent{},
		&TunnelState{},
		&AdminRefreshToken{},
	)
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
