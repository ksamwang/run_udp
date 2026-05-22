package packet

import (
	"errors"
	"sort"
	"sync"
	"time"

	"udp_tunnel_demo/internal/store"
)

const (
	LinkPathP2P   = "p2p"
	LinkPathRelay = "relay"

	DefaultMaxPeerSessions = 64
	DefaultKeepAliveEvery  = 15 * time.Second
	DefaultIdleTimeout     = 2 * time.Minute
)

var (
	ErrPeerSessionLimit = errors.New("peer session limit reached")
	ErrPeerNotFound     = errors.New("peer session not found")
	ErrLinkUnavailable  = errors.New("packet link unavailable")
)

type PeerEndpoint struct {
	DeviceID string
	Addr     string
	UPnPAddr string
}

type LinkConfig struct {
	DeviceID        string
	ForceRelay      bool
	MaxPeerSessions int
	KeepAliveEvery  time.Duration
	IdleTimeout     time.Duration
	Now             func() time.Time
}

type LinkManager struct {
	deviceID       string
	forceRelay     bool
	maxSessions    int
	keepAliveEvery time.Duration
	idleTimeout    time.Duration
	now            func() time.Time

	mu       sync.Mutex
	sessions map[string]*PeerSession
	stats    LinkStats
}

type PeerSession struct {
	PeerID       string
	Profile      string
	Path         string
	Endpoint     PeerEndpoint
	Relay        PeerEndpoint
	State        string
	Attempt      int
	LastError    string
	CreatedAt    time.Time
	LastSeenAt   time.Time
	LastSentAt   time.Time
	LastRecvAt   time.Time
	NetworkEpoch uint64
	TxBytes      uint64
	RxBytes      uint64
	RelayBytes   uint64
	KeepAlives   uint64
}

type LinkStats struct {
	ActiveSessions uint64 `json:"active_sessions"`
	Created        uint64 `json:"created"`
	Rebuilt        uint64 `json:"rebuilt"`
	Closed         uint64 `json:"closed"`
	TxBytes        uint64 `json:"tx_bytes"`
	RxBytes        uint64 `json:"rx_bytes"`
	RelayBytes     uint64 `json:"relay_bytes"`
	KeepAlives     uint64 `json:"keepalives"`
	LimitDrops     uint64 `json:"limit_drops"`
}

type LinkFrame struct {
	PeerID  string
	Path    string
	Payload []byte
}

func NewLinkManager(cfg LinkConfig) *LinkManager {
	maxSessions := cfg.MaxPeerSessions
	if maxSessions <= 0 {
		maxSessions = DefaultMaxPeerSessions
	}
	keepAliveEvery := cfg.KeepAliveEvery
	if keepAliveEvery <= 0 {
		keepAliveEvery = DefaultKeepAliveEvery
	}
	idleTimeout := cfg.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &LinkManager{
		deviceID: cfg.DeviceID, forceRelay: cfg.ForceRelay, maxSessions: maxSessions,
		keepAliveEvery: keepAliveEvery, idleTimeout: idleTimeout, now: now,
		sessions: map[string]*PeerSession{},
	}
}

func (m *LinkManager) UpsertPeer(peer PeerEndpoint, relay PeerEndpoint, p2pReady bool) (PeerSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	key := peer.DeviceID
	if key == "" {
		return PeerSession{}, ErrPeerNotFound
	}
	session, exists := m.sessions[key]
	if !exists {
		if len(m.sessions) >= m.maxSessions {
			m.stats.LimitDrops++
			return PeerSession{}, ErrPeerSessionLimit
		}
		session = &PeerSession{PeerID: key, Profile: store.ProfileLANPacket, CreatedAt: now}
		m.sessions[key] = session
		m.stats.Created++
	}
	path := LinkPathRelay
	if !m.forceRelay && p2pReady {
		path = LinkPathP2P
	}
	if session.Path != "" && session.Path != path {
		m.stats.Rebuilt++
	}
	session.Endpoint = peer
	session.Relay = relay
	session.Path = path
	session.State = path
	session.LastError = ""
	session.LastSeenAt = now
	return *session, nil
}

func (m *LinkManager) Send(peerID string, payload []byte) (LinkFrame, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[peerID]
	if !ok {
		return LinkFrame{}, ErrPeerNotFound
	}
	if session.State == "" || session.Path == "" {
		return LinkFrame{}, ErrLinkUnavailable
	}
	now := m.now()
	session.LastSentAt = now
	session.TxBytes += uint64(len(payload))
	if session.Path == LinkPathRelay {
		session.RelayBytes += uint64(len(payload))
		m.stats.RelayBytes += uint64(len(payload))
	}
	m.stats.TxBytes += uint64(len(payload))
	return LinkFrame{PeerID: peerID, Path: session.Path, Payload: append([]byte(nil), payload...)}, nil
}

func (m *LinkManager) Receive(peerID string, payload []byte, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[peerID]
	if !ok {
		return ErrPeerNotFound
	}
	session.LastRecvAt = m.now()
	session.RxBytes += uint64(len(payload))
	if path != "" && path != session.Path {
		session.Path = path
		session.State = path
	}
	m.stats.RxBytes += uint64(len(payload))
	return nil
}

func (m *LinkManager) DueKeepAlives() []LinkFrame {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	out := make([]LinkFrame, 0)
	for _, session := range m.sessions {
		if session.Path == "" || session.State == "" {
			continue
		}
		if session.LastSentAt.IsZero() || now.Sub(session.LastSentAt) >= m.keepAliveEvery {
			session.LastSentAt = now
			session.KeepAlives++
			m.stats.KeepAlives++
			out = append(out, LinkFrame{PeerID: session.PeerID, Path: session.Path})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeerID < out[j].PeerID })
	return out
}

func (m *LinkManager) RebuildForNetworkChange() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for _, session := range m.sessions {
		session.NetworkEpoch++
		session.Path = ""
		session.State = "rebuilding"
		session.LastError = "network_changed"
		session.LastSeenAt = now
		m.stats.Rebuilt++
	}
}

func (m *LinkManager) CleanupIdle() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	closed := make([]string, 0)
	for peerID, session := range m.sessions {
		if !session.LastSeenAt.IsZero() && now.Sub(session.LastSeenAt) <= m.idleTimeout {
			continue
		}
		if session.LastSeenAt.IsZero() && now.Sub(session.CreatedAt) <= m.idleTimeout {
			continue
		}
		delete(m.sessions, peerID)
		closed = append(closed, peerID)
		m.stats.Closed++
	}
	sort.Strings(closed)
	return closed
}

func (m *LinkManager) Session(peerID string) (PeerSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[peerID]
	if !ok {
		return PeerSession{}, false
	}
	return *session, true
}

func (m *LinkManager) Sessions() []PeerSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PeerSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		out = append(out, *session)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeerID < out[j].PeerID })
	return out
}

func (m *LinkManager) Stats() LinkStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	stats := m.stats
	stats.ActiveSessions = uint64(len(m.sessions))
	return stats
}
