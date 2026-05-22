package server

import (
	"context"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"udp_tunnel_demo/internal/config"
	"udp_tunnel_demo/internal/controlstore"
	"udp_tunnel_demo/internal/secure"
)

type peer struct {
	id        string
	addr      *net.UDPAddr
	upnpAddr  string
	want      string
	profile   string
	lastSeen  time.Time
	sessionID int64
}

type pairRoute struct {
	dst       *net.UDPAddr
	lastSeen  time.Time
	sessionID int64
}

type App struct {
	cfg   config.Server
	db    controlstore.Store
	codec *secure.Codec

	startTime     time.Time
	totalRegister atomic.Uint64
	totalPaired   atomic.Uint64
	totalRelayed  atomic.Uint64

	mu       sync.Mutex
	peers    map[string]map[string]*peer // from -> want -> peer，一台设备可同时申请多个 peer
	pairByID map[string]int64
	pairs    sync.Map // src address string -> pairRoute

	cfgMu sync.RWMutex
}

type agentTunnelReport struct {
	Peer             string `json:"peer"`
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
}

type agentBootstrapResponse struct {
	DeviceID     string `json:"device_id"`
	DeviceName   string `json:"device_name"`
	Server       string `json:"server"`
	ServerHTTP   string `json:"server_http"`
	PSK          string `json:"psk"`
	STUNAltPort  int    `json:"stun_alt_port"`
	NoUPnP       bool   `json:"no_upnp"`
	UPnPTimeout  string `json:"upnp_timeout"`
	LogLevel     string `json:"log_level"`
	TrayEnabled  bool   `json:"tray_enabled"`
	PunchTimeout string `json:"punch_timeout"`
	ForceRelay   bool   `json:"force_relay"`
	AllowLegacy  bool   `json:"allow_legacy"`
}

type clientReleaseResponse struct {
	Version                 string `json:"version"`
	URL                     string `json:"url"`
	SHA256                  string `json:"sha256"`
	PublishedAt             string `json:"published_at"`
	Notes                   string `json:"notes"`
	MinimumSupportedVersion string `json:"minimum_supported_version"`
}

func New(cfg config.Server) (*App, error) {
	db, err := controlstore.Open(controlstore.Config{
		DSN: cfg.ControlDatabaseDSN,
	})
	if err != nil {
		return nil, err
	}

	var codec *secure.Codec
	if cfg.PSK != "" {
		codec, err = secure.NewCodec(cfg.PSK)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
	} else {
		log.Printf("WARN product PSK is empty; secure UDP frames and agent auth are disabled")
		cfg.AllowLegacy = true
	}

	a := &App{
		cfg:       cfg,
		db:        db,
		codec:     codec,
		startTime: time.Now(),
		peers:     map[string]map[string]*peer{},
		pairByID:  map[string]int64{},
	}
	if err := a.ensureAdminUser(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := a.applyStoredSettings(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return a, nil
}

func (a *App) Close() error {
	if a == nil || a.db == nil {
		return nil
	}
	return a.db.Close()
}

func (a *App) Run(ctx context.Context) error {
	go a.cleanupLoop()
	go a.runHTTP()
	go a.runStunAlt()
	go func() {
		<-ctx.Done()
		_ = a.Close()
	}()
	a.runUDP()
	return nil
}
