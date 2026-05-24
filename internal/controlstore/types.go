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
	PutSystemSetting(ctx context.Context, key, value string) error
	GetSystemSetting(ctx context.Context, key string) (string, error)
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
	ListAuditEvents(ctx context.Context, filter store.AuditFilter) ([]store.AuditEvent, error)
	UpsertAdminUser(ctx context.Context, user store.AdminUser) error
	GetAdminUserByUsername(ctx context.Context, username string) (store.AdminUser, error)
	GetAdminUserByID(ctx context.Context, id string) (store.AdminUser, error)
	UpdateAdminPassword(ctx context.Context, userID, passwordHash string) error
	ClearAdminPasswordChangeRequired(ctx context.Context, userID string) error
	CreateAdminRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time, userAgent, ip string) error
	GetAdminRefreshToken(ctx context.Context, tokenHash string) (store.AdminRefreshToken, error)
	TouchAdminRefreshToken(ctx context.Context, id int64) error
	RevokeAdminRefreshToken(ctx context.Context, tokenHash string) error
	RevokeAdminRefreshTokensByUser(ctx context.Context, userID string) error
	RevokeExpiredAdminRefreshTokens(ctx context.Context, cutoff time.Time) error
	EnsureDefaultVirtualNetwork(ctx context.Context) (store.VirtualNetwork, error)
	ListVirtualNetworks(ctx context.Context) ([]store.VirtualNetwork, error)
	CreateVirtualNetwork(ctx context.Context, network store.VirtualNetwork) (store.VirtualNetwork, error)
	UpdateVirtualNetwork(ctx context.Context, id int64, network store.VirtualNetwork) error
	DeleteVirtualNetwork(ctx context.Context, id int64) error
	UpsertVirtualAddress(ctx context.Context, address store.VirtualAddress) error
	ListVirtualAddresses(ctx context.Context, networkID int64) ([]store.VirtualAddress, error)
	GetVirtualAddress(ctx context.Context, networkID int64, deviceID string) (store.VirtualAddress, error)
	GetVirtualAddressByDevice(ctx context.Context, deviceID string) (store.VirtualAddress, error)
	DeleteVirtualAddress(ctx context.Context, networkID int64, deviceID string) error
	UpsertVirtualDeviceKey(ctx context.Context, key store.VirtualDeviceKey) error
	GetVirtualDeviceKey(ctx context.Context, deviceID string) (store.VirtualDeviceKey, error)
	ListVirtualDeviceKeys(ctx context.Context) ([]store.VirtualDeviceKey, error)
	UpsertVirtualDeviceGroup(ctx context.Context, group store.VirtualDeviceGroup) (store.VirtualDeviceGroup, error)
	DeleteVirtualDeviceGroup(ctx context.Context, id string) error
	ListVirtualDeviceGroups(ctx context.Context) ([]store.VirtualDeviceGroup, error)
	SetVirtualDeviceGroupMembers(ctx context.Context, groupID string, deviceIDs []string) error
	ListVirtualDeviceGroupMembers(ctx context.Context, groupID string) ([]store.VirtualDeviceGroupMember, error)
	CreateVirtualACLRule(ctx context.Context, rule store.VirtualACLRule) (store.VirtualACLRule, error)
	ListVirtualACLRules(ctx context.Context, networkID int64) ([]store.VirtualACLRule, error)
	UpdateVirtualACLRule(ctx context.Context, id int64, rule store.VirtualACLRule) error
	DeleteVirtualACLRule(ctx context.Context, id int64) error
	UpsertVirtualRoute(ctx context.Context, route store.VirtualRoute) error
	ListVirtualRoutes(ctx context.Context, networkID int64, deviceID string) ([]store.VirtualRoute, error)
	DeleteVirtualRoute(ctx context.Context, id int64) error
	PutVirtualPeerState(ctx context.Context, state store.VirtualPeerState) error
	ListVirtualPeerStates(ctx context.Context, networkID int64) ([]store.VirtualPeerState, error)
}
