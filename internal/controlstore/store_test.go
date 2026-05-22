package controlstore

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"udp_tunnel_demo/internal/store"
)

func TestMySQLStoreIntegration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("UDP_TUNNEL_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("set UDP_TUNNEL_MYSQL_DSN to run MySQL integration test")
	}
	s, err := Open(Config{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Cleanup(func() { _ = s.Close() })
	t.Cleanup(func() { cleanupMySQL(t, s.(*MySQLStore)) })
	cleanupMySQL(t, s.(*MySQLStore))

	if err := exerciseStore(ctx, s); err != nil {
		t.Fatal(err)
	}
}

func exerciseStore(ctx context.Context, s Store) error {
	if err := s.PutMeta(ctx, "codex_test_meta", "hash"); err != nil {
		return err
	}
	if got, err := s.GetMeta(ctx, "codex_test_meta"); err != nil || got != "hash" {
		return failf("meta got=%q err=%v", got, err)
	}
	if err := s.PutSystemSetting(ctx, "codex_test_setting", "90s"); err != nil {
		return err
	}
	if got, err := s.GetSystemSetting(ctx, "codex_test_setting"); err != nil || got != "90s" {
		return failf("system setting got=%q err=%v", got, err)
	}
	if err := s.UpsertDevice(ctx, "codex-test-A", "office-pc", "1.1.1.1:1", "2.2.2.2:2", "codex-test-B", true); err != nil {
		return err
	}
	if err := s.UpsertDevice(ctx, "codex-test-A", "", "", "", "", true); err != nil {
		return err
	}
	a, err := s.GetDevice(ctx, "codex-test-A")
	if err != nil {
		return err
	}
	if a.Name != "office-pc" || a.Addr != "1.1.1.1:1" || a.UpnpAddr != "2.2.2.2:2" || a.Want != "codex-test-B" {
		return failf("upsert should preserve non-empty fields: %+v", a)
	}
	if err := s.UpsertDevice(ctx, "codex-test-B", "codex-test-B", "", "", "", true); err != nil {
		return err
	}
	rule, err := s.CreateRule(ctx, store.ForwardRule{
		Name: "codex-test-rdp", SourceID: "codex-test-A", TargetID: "codex-test-B", LocalPort: 13389,
		TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	})
	if err != nil {
		return err
	}
	rules, err := s.RulesForDevice(ctx, "codex-test-A")
	if err != nil {
		return err
	}
	if len(rules) != 1 || rules[0].Profile != store.ProfileInteractive {
		return failf("unexpected rules: %+v", rules)
	}
	conflict, err := s.LocalPortConflict(ctx, "codex-test-A", 13389, 0)
	if err != nil || !conflict {
		return failf("expected local port conflict, conflict=%v err=%v", conflict, err)
	}
	if n, err := s.EnabledRuleReferenceCount(ctx, "codex-test-A"); err != nil || n != 1 {
		return failf("reference count=%d err=%v", n, err)
	}
	if err := s.UpdateRule(ctx, rule.ID, store.ForwardRule{
		Name: "codex-test-rdp2", SourceID: "codex-test-A", TargetID: "codex-test-B", Profile: store.ProfileBulk,
		LocalPort: 13390, TargetHost: "127.0.0.1", TargetPort: 3389, Enabled: true,
	}); err != nil {
		return err
	}
	sessionID, err := s.StartSession(ctx, "codex-test-A", "codex-test-B", store.ProfileBulk, "pending")
	if err != nil {
		return err
	}
	if err := s.TouchSession(ctx, sessionID, 128); err != nil {
		return err
	}
	if err := s.UpdateSessionPathForPair(ctx, "codex-test-B", "codex-test-A", store.ProfileBulk, "p2p"); err != nil {
		return err
	}
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		return err
	}
	if len(sessions) != 1 || sessions[0].Path != "p2p" || sessions[0].RelayBytes != 128 {
		return failf("unexpected sessions: %+v", sessions)
	}
	if err := s.PutTunnelState(ctx, store.TunnelState{
		DeviceID: "codex-test-A", PeerID: "codex-test-B", Profile: store.ProfileBulk, State: "p2p", Via: "p2p",
		NATType: "cone", PublicAddr: "1.1.1.1:1", ConvID: 1, RTTMs: 12,
		LastError: "", Attempt: 1, NextRetryAt: "", LastTransitionAt: "2026-05-22T12:00:00Z",
	}); err != nil {
		return err
	}
	states, err := s.ListTunnelStates(ctx)
	if err != nil {
		return err
	}
	if len(states) != 1 || states[0].Profile != store.ProfileBulk || states[0].NATType != "cone" {
		return failf("unexpected states: %+v", states)
	}
	metrics, err := s.Metrics(ctx)
	if err != nil {
		return err
	}
	if metrics.Devices != 2 || metrics.ForwardRules != 1 || metrics.ActiveSessions != 1 || metrics.RelayBytes != 128 {
		return failf("bad metrics: %+v", metrics)
	}
	if err := s.CreateAdminRefreshToken(ctx, "codex-test-admin", "codex-test-token-hash", time.Now().Add(time.Hour), "ua", "127.0.0.1"); err != nil {
		return err
	}
	token, err := s.GetAdminRefreshToken(ctx, "codex-test-token-hash")
	if err != nil {
		return err
	}
	if token.UserID != "codex-test-admin" {
		return failf("bad token: %+v", token)
	}
	if err := s.TouchAdminRefreshToken(ctx, token.ID); err != nil {
		return err
	}
	if err := s.RevokeAdminRefreshToken(ctx, "codex-test-token-hash"); err != nil {
		return err
	}
	token, err = s.GetAdminRefreshToken(ctx, "codex-test-token-hash")
	if err != nil {
		return err
	}
	if token.RevokedAt == "" {
		return failf("token should be revoked: %+v", token)
	}
	if err := s.Audit(ctx, "codex-test", "ok"); err != nil {
		return err
	}
	if err := s.UpsertAdminUser(ctx, store.AdminUser{
		ID: "codex-test-admin", Username: "codex-admin", Name: "Codex Admin", Role: "admin", PasswordHash: "hash",
	}); err != nil {
		return err
	}
	admin, err := s.GetAdminUserByUsername(ctx, "codex-admin")
	if err != nil {
		return err
	}
	if admin.ID != "codex-test-admin" || admin.PasswordHash != "hash" {
		return failf("bad admin user: %+v", admin)
	}
	if err := s.UpdateAdminPassword(ctx, "codex-test-admin", "next-hash"); err != nil {
		return err
	}
	admin, err = s.GetAdminUserByID(ctx, "codex-test-admin")
	if err != nil {
		return err
	}
	if admin.PasswordHash != "next-hash" {
		return failf("admin password not updated: %+v", admin)
	}
	if err := s.SetDeviceEnabled(ctx, "codex-test-B", false); err != nil {
		return err
	}
	b, err := s.GetDevice(ctx, "codex-test-B")
	if err != nil {
		return err
	}
	if b.Enabled {
		return failf("device should be disabled: %+v", b)
	}
	if err := exerciseVirtualLANStore(ctx, s); err != nil {
		return err
	}
	return nil
}

func exerciseVirtualLANStore(ctx context.Context, s Store) error {
	defaultNetwork, err := s.EnsureDefaultVirtualNetwork(ctx)
	if err != nil {
		return err
	}
	if defaultNetwork.CIDR != "172.16.10.0/24" || !defaultNetwork.Enabled {
		return failf("bad default network: %+v", defaultNetwork)
	}
	defaultNetwork2, err := s.EnsureDefaultVirtualNetwork(ctx)
	if err != nil {
		return err
	}
	if defaultNetwork2.ID != defaultNetwork.ID {
		return failf("default network should be stable: first=%+v second=%+v", defaultNetwork, defaultNetwork2)
	}
	network2, err := s.CreateVirtualNetwork(ctx, store.VirtualNetwork{Name: "codex-test-lab", CIDR: "172.16.20.0/24", Enabled: true})
	if err != nil {
		return err
	}
	networks, err := s.ListVirtualNetworks(ctx)
	if err != nil {
		return err
	}
	if len(networks) < 2 {
		return failf("expected at least two virtual networks: %+v", networks)
	}
	if err := s.UpdateVirtualNetwork(ctx, network2.ID, store.VirtualNetwork{Name: "codex-test-lab2", CIDR: "172.16.21.0/24", Enabled: false}); err != nil {
		return err
	}
	if err := s.UpsertVirtualAddress(ctx, store.VirtualAddress{
		DeviceID: "codex-test-A", NetworkID: defaultNetwork.ID, VirtualIP: "172.16.10.10", Hostname: "office-a", DNSEnabled: true,
	}); err != nil {
		return err
	}
	if err := s.UpsertVirtualAddress(ctx, store.VirtualAddress{
		DeviceID: "codex-test-B", NetworkID: defaultNetwork.ID, VirtualIP: "172.16.10.11",
	}); err != nil {
		return err
	}
	if err := s.UpsertVirtualAddress(ctx, store.VirtualAddress{
		DeviceID: "codex-test-A", NetworkID: network2.ID, VirtualIP: "172.16.21.10", Hostname: "office-a-lab",
	}); err != nil {
		return err
	}
	addr, err := s.GetVirtualAddress(ctx, defaultNetwork.ID, "codex-test-A")
	if err != nil {
		return err
	}
	if addr.VirtualIP != "172.16.10.10" || addr.Hostname != "office-a" || !addr.DNSEnabled {
		return failf("bad virtual address: %+v", addr)
	}
	addresses, err := s.ListVirtualAddresses(ctx, 0)
	if err != nil {
		return err
	}
	if len(addresses) != 3 {
		return failf("expected three virtual addresses: %+v", addresses)
	}
	acl, err := s.CreateVirtualACLRule(ctx, store.VirtualACLRule{
		NetworkID: defaultNetwork.ID, SourceDeviceID: "codex-test-A", TargetDeviceID: "codex-test-B",
		Protocol: "tcp", PortStart: 3389, PortEnd: 3389, Action: "deny", Enabled: true,
	})
	if err != nil {
		return err
	}
	rules, err := s.ListVirtualACLRules(ctx, defaultNetwork.ID)
	if err != nil {
		return err
	}
	if len(rules) != 1 || rules[0].Action != "deny" {
		return failf("bad acl rules: %+v", rules)
	}
	if err := s.UpdateVirtualACLRule(ctx, acl.ID, store.VirtualACLRule{
		NetworkID: defaultNetwork.ID, SourceDeviceID: "codex-test-A", TargetDeviceID: "codex-test-B",
		Protocol: "tcp", PortStart: 22, PortEnd: 22, Action: "allow", Enabled: false,
	}); err != nil {
		return err
	}
	if err := s.UpsertVirtualRoute(ctx, store.VirtualRoute{
		DeviceID: "codex-test-A", NetworkID: defaultNetwork.ID, CIDR: "10.10.0.0/16", Advertise: true, Accept: false,
	}); err != nil {
		return err
	}
	if err := s.UpsertVirtualRoute(ctx, store.VirtualRoute{
		DeviceID: "codex-test-A", NetworkID: defaultNetwork.ID, CIDR: "10.10.0.0/16", Advertise: false, Accept: true,
	}); err != nil {
		return err
	}
	routes, err := s.ListVirtualRoutes(ctx, defaultNetwork.ID, "codex-test-A")
	if err != nil {
		return err
	}
	if len(routes) != 1 || routes[0].Advertise || !routes[0].Accept {
		return failf("bad virtual routes: %+v", routes)
	}
	if err := s.PutVirtualPeerState(ctx, store.VirtualPeerState{
		DeviceID: "codex-test-A", PeerID: "codex-test-B", NetworkID: defaultNetwork.ID,
		State: "connected", Path: "p2p", RTTMs: 10, TxBytes: 100, RxBytes: 200, LastHandshakeAt: "2026-05-23T01:00:00Z",
	}); err != nil {
		return err
	}
	peerStates, err := s.ListVirtualPeerStates(ctx, defaultNetwork.ID)
	if err != nil {
		return err
	}
	if len(peerStates) != 1 || peerStates[0].Path != "p2p" || peerStates[0].TxBytes != 100 {
		return failf("bad virtual peer states: %+v", peerStates)
	}
	if err := s.DeleteVirtualACLRule(ctx, acl.ID); err != nil {
		return err
	}
	if err := s.DeleteVirtualNetwork(ctx, network2.ID); err != nil {
		return err
	}
	return nil
}

func cleanupMySQL(t *testing.T, s *MySQLStore) {
	t.Helper()
	statements := []string{
		"DELETE FROM virtual_peer_states WHERE device_id LIKE ? OR peer_id LIKE ?",
		"DELETE FROM virtual_routes WHERE device_id LIKE ? OR cidr LIKE ?",
		"DELETE FROM virtual_acl_rules WHERE source_device_id LIKE ? OR target_device_id LIKE ? OR source_group_id LIKE ? OR target_group_id LIKE ?",
		"DELETE FROM virtual_addresses WHERE device_id LIKE ? OR virtual_ip LIKE ? OR hostname LIKE ?",
		"DELETE FROM virtual_networks WHERE name LIKE ? OR cidr = ?",
		"DELETE FROM admin_refresh_tokens WHERE user_id = ? OR token_hash = ?",
		"DELETE FROM admin_users WHERE id = ? OR username = ?",
		"DELETE FROM audit_events WHERE kind = ?",
		"DELETE FROM tunnel_states WHERE device_id LIKE ? OR peer_id LIKE ?",
		"DELETE FROM sessions WHERE source_id LIKE ? OR target_id LIKE ?",
		"DELETE FROM forward_rules WHERE source_id LIKE ? OR target_id LIKE ? OR name LIKE ?",
		"DELETE FROM devices WHERE id LIKE ?",
		"DELETE FROM system_settings WHERE `key` = ?",
		"DELETE FROM meta WHERE `key` = ?",
	}
	args := [][]any{
		{"codex-test-%", "codex-test-%"},
		{"codex-test-%", "10.10.%"},
		{"codex-test-%", "codex-test-%", "codex-test-%", "codex-test-%"},
		{"codex-test-%", "172.16.%", "office-%"},
		{"codex-test-%", "172.16.21.0/24"},
		{"codex-test-admin", "codex-test-token-hash"},
		{"codex-test-admin", "codex-admin"},
		{"codex-test"},
		{"codex-test-%", "codex-test-%"},
		{"codex-test-%", "codex-test-%"},
		{"codex-test-%", "codex-test-%", "codex-test-%"},
		{"codex-test-%"},
		{"codex_test_setting"},
		{"codex_test_meta"},
	}
	for i, stmt := range statements {
		if err := s.db.Exec(stmt, args[i]...).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func failf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
