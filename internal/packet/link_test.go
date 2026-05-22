package packet

import (
	"errors"
	"testing"
	"time"
)

func TestLinkManagerPrefersP2PAndFallsBackToRelay(t *testing.T) {
	now := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	manager := NewLinkManager(LinkConfig{DeviceID: "dev-a", Now: func() time.Time { return now }})

	p2p, err := manager.UpsertPeer(PeerEndpoint{DeviceID: "dev-b", Addr: "1.1.1.1:1000"}, PeerEndpoint{Addr: "server:17000"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if p2p.Path != LinkPathP2P || p2p.State != LinkPathP2P {
		t.Fatalf("expected p2p, got %+v", p2p)
	}
	relay, err := manager.UpsertPeer(PeerEndpoint{DeviceID: "dev-c", Addr: "2.2.2.2:1000"}, PeerEndpoint{Addr: "server:17000"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if relay.Path != LinkPathRelay || relay.State != LinkPathRelay {
		t.Fatalf("expected relay, got %+v", relay)
	}
}

func TestLinkManagerForceRelay(t *testing.T) {
	manager := NewLinkManager(LinkConfig{DeviceID: "dev-a", ForceRelay: true})
	session, err := manager.UpsertPeer(PeerEndpoint{DeviceID: "dev-b", Addr: "1.1.1.1:1000"}, PeerEndpoint{Addr: "server:17000"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if session.Path != LinkPathRelay {
		t.Fatalf("expected relay, got %+v", session)
	}
}

func TestLinkManagerSendStatsAndRelayAccounting(t *testing.T) {
	manager := NewLinkManager(LinkConfig{DeviceID: "dev-a"})
	if _, err := manager.UpsertPeer(PeerEndpoint{DeviceID: "dev-b"}, PeerEndpoint{Addr: "server:17000"}, false); err != nil {
		t.Fatal(err)
	}
	frame, err := manager.Send("dev-b", []byte("packet"))
	if err != nil {
		t.Fatal(err)
	}
	if frame.Path != LinkPathRelay || string(frame.Payload) != "packet" {
		t.Fatalf("bad frame: %+v", frame)
	}
	if err := manager.Receive("dev-b", []byte("reply"), LinkPathRelay); err != nil {
		t.Fatal(err)
	}
	stats := manager.Stats()
	if stats.TxBytes != 6 || stats.RxBytes != 5 || stats.RelayBytes != 6 || stats.ActiveSessions != 1 {
		t.Fatalf("bad stats: %+v", stats)
	}
	session, _ := manager.Session("dev-b")
	if session.TxBytes != 6 || session.RxBytes != 5 || session.RelayBytes != 6 {
		t.Fatalf("bad session counters: %+v", session)
	}
}

func TestLinkManagerKeepAliveAndNetworkRebuild(t *testing.T) {
	now := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	manager := NewLinkManager(LinkConfig{
		DeviceID: "dev-a", KeepAliveEvery: time.Second,
		Now: func() time.Time { return now },
	})
	if _, err := manager.UpsertPeer(PeerEndpoint{DeviceID: "dev-b"}, PeerEndpoint{}, true); err != nil {
		t.Fatal(err)
	}
	if frames := manager.DueKeepAlives(); len(frames) != 1 || frames[0].PeerID != "dev-b" {
		t.Fatalf("expected keepalive, got %+v", frames)
	}
	if frames := manager.DueKeepAlives(); len(frames) != 0 {
		t.Fatalf("expected no immediate keepalive, got %+v", frames)
	}
	now = now.Add(2 * time.Second)
	if frames := manager.DueKeepAlives(); len(frames) != 1 {
		t.Fatalf("expected later keepalive, got %+v", frames)
	}

	manager.RebuildForNetworkChange()
	session, ok := manager.Session("dev-b")
	if !ok || session.State != "rebuilding" || session.Path != "" || session.NetworkEpoch != 1 {
		t.Fatalf("expected rebuilding session, got %+v ok=%v", session, ok)
	}
	if frames := manager.DueKeepAlives(); len(frames) != 0 {
		t.Fatalf("rebuilding sessions should not keepalive: %+v", frames)
	}
}

func TestLinkManagerLimitsAndCleanup(t *testing.T) {
	now := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	manager := NewLinkManager(LinkConfig{
		DeviceID: "dev-a", MaxPeerSessions: 1, IdleTimeout: time.Second,
		Now: func() time.Time { return now },
	})
	if _, err := manager.UpsertPeer(PeerEndpoint{DeviceID: "dev-b"}, PeerEndpoint{}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.UpsertPeer(PeerEndpoint{DeviceID: "dev-c"}, PeerEndpoint{}, true); !errors.Is(err, ErrPeerSessionLimit) {
		t.Fatalf("expected session limit, got %v", err)
	}
	if got := manager.Stats().LimitDrops; got != 1 {
		t.Fatalf("limit drops=%d", got)
	}
	now = now.Add(2 * time.Second)
	closed := manager.CleanupIdle()
	if len(closed) != 1 || closed[0] != "dev-b" {
		t.Fatalf("bad cleanup: %+v", closed)
	}
	if stats := manager.Stats(); stats.ActiveSessions != 0 || stats.Closed != 1 {
		t.Fatalf("bad stats after cleanup: %+v", stats)
	}
}

func TestLinkManagerUnknownPeer(t *testing.T) {
	manager := NewLinkManager(LinkConfig{DeviceID: "dev-a"})
	if _, err := manager.Send("missing", []byte("packet")); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("expected peer not found, got %v", err)
	}
	if err := manager.Receive("missing", []byte("packet"), LinkPathP2P); !errors.Is(err, ErrPeerNotFound) {
		t.Fatalf("expected peer not found, got %v", err)
	}
}
