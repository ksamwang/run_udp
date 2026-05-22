package controlstore

import (
	"context"
	"time"

	"udp_tunnel_demo/internal/store"
)

type Store interface {
	Close() error
	PutMeta(ctx context.Context, key, value string) error
	GetMeta(ctx context.Context, key string) (string, error)
	UpsertDevice(ctx context.Context, id, name, addr, upnpAddr, want string, online bool) error
	MarkOfflineBefore(ctx context.Context, cutoff time.Time) error
	ListDevices(ctx context.Context) ([]store.Device, error)
	GetDevice(ctx context.Context, id string) (store.Device, error)
	SetDeviceEnabled(ctx context.Context, id string, enabled bool) error
	DeleteDevice(ctx context.Context, id string) error
	EnabledRuleReferenceCount(ctx context.Context, deviceID string) (int, error)
	ListRules(ctx context.Context) ([]store.ForwardRule, error)
	RulesForDevice(ctx context.Context, deviceID string) ([]store.ForwardRule, error)
	CreateRule(ctx context.Context, rule store.ForwardRule) (store.ForwardRule, error)
	UpdateRule(ctx context.Context, id int64, rule store.ForwardRule) error
	DeleteRule(ctx context.Context, id int64) error
	StartSession(ctx context.Context, sourceID, targetID, profile, path string) (int64, error)
	TouchSession(ctx context.Context, id int64, relayBytes int64) error
	UpdateSessionPathForPair(ctx context.Context, aID, bID, profile, path string) error
	EndIdleSessions(ctx context.Context, cutoff time.Time) error
	ListSessions(ctx context.Context) ([]store.Session, error)
	Metrics(ctx context.Context) (store.Metrics, error)
	PutTunnelState(ctx context.Context, ts store.TunnelState) error
	ListTunnelStates(ctx context.Context) ([]store.TunnelState, error)
	LocalPortConflict(ctx context.Context, sourceID string, localPort int, excludeID int64) (bool, error)
	Audit(ctx context.Context, kind, detail string) error
	CreateAdminRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time, userAgent, ip string) error
	GetAdminRefreshToken(ctx context.Context, tokenHash string) (store.AdminRefreshToken, error)
	TouchAdminRefreshToken(ctx context.Context, id int64) error
	RevokeAdminRefreshToken(ctx context.Context, tokenHash string) error
	RevokeExpiredAdminRefreshTokens(ctx context.Context, cutoff time.Time) error
}
