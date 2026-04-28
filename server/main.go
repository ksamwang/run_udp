package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"

	"udp_tunnel_demo/internal/config"
	"udp_tunnel_demo/internal/protocol"
	"udp_tunnel_demo/internal/secure"
	"udp_tunnel_demo/internal/store"
)

//go:embed web/*
var webFS embed.FS

type peer struct {
	id        string
	addr      *net.UDPAddr
	upnpAddr  string
	want      string
	lastSeen  time.Time
	sessionID int64
}

type pairRoute struct {
	dst       *net.UDPAddr
	lastSeen  time.Time
	sessionID int64
}

type app struct {
	cfg   config.Server
	db    *store.Store
	codec *secure.Codec

	startTime     time.Time
	totalRegister atomic.Uint64
	totalPaired   atomic.Uint64
	totalRelayed  atomic.Uint64

	mu       sync.Mutex
	peers    map[string]map[string]*peer // from -> want -> peer，一台设备可同时申请多个 peer
	pairByID map[string]int64
	pairs    sync.Map // src address string -> pairRoute

	authMu   sync.Mutex
	sessions map[string]time.Time
	cfgMu    sync.RWMutex
}

type apiError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"error"`
}

func (e *apiError) Error() string { return e.Message }

type agentTunnelReport struct {
	Peer       string `json:"peer"`
	State      string `json:"state"`
	Via        string `json:"via"`
	NATType    string `json:"nat_type"`
	PublicAddr string `json:"public_addr"`
	ConvID     int64  `json:"conv_id"`
	RTTMs      int    `json:"rtt_ms"`
	LastError  string `json:"last_error"`
}

type agentBootstrapResponse struct {
	DeviceID     string `json:"device_id"`
	DeviceName   string `json:"device_name"`
	Server       string `json:"server"`
	ServerHTTP   string `json:"server_http"`
	STUNAltPort  int    `json:"stun_alt_port"`
	NoUPnP       bool   `json:"no_upnp"`
	UPnPTimeout  string `json:"upnp_timeout"`
	LogLevel     string `json:"log_level"`
	TrayEnabled  bool   `json:"tray_enabled"`
	PunchTimeout string `json:"punch_timeout"`
	ForceRelay   bool   `json:"force_relay"`
	AllowLegacy  bool   `json:"allow_legacy"`
}

func main() {
	cfg := config.DefaultServer()
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	configPath := fs.String("config", "server.json", "server config file")
	udpAddr := fs.String("listen", cfg.UDPListen, "UDP listen address")
	stunAlt := fs.String("stun-alt", cfg.StunAltListen, "STUN alternate UDP listen address")
	httpAddr := fs.String("http", cfg.HTTPListen, "HTTP listen address")
	dbPath := fs.String("db", cfg.DatabasePath, "SQLite database path")
	psk := fs.String("psk", cfg.PSK, "deployment pre-shared key")
	adminPassword := fs.String("admin-password", cfg.AdminPassword, "initial admin password")
	adminHash := fs.String("admin-password-hash", cfg.AdminPasswordHash, "bcrypt admin password hash")
	peerTTL := fs.Duration("peer-ttl", cfg.PeerTTL, "peer TTL")
	pairTTL := fs.Duration("pair-ttl", cfg.PairTTL, "pair TTL")
	relayIdle := fs.Duration("relay-idle-timeout", cfg.RelayIdleTimeout, "relay idle timeout")
	allowRelay := fs.Bool("allow-relay", cfg.AllowRelay, "allow TURN relay forwarding")
	allowLegacy := fs.Bool("allow-legacy", cfg.AllowLegacy, "allow legacy plaintext JSON UDP protocol")
	fs.Parse(os.Args[1:])

	_ = config.LoadJSON(*configPath, &cfg)
	if flagSet(fs, "listen") {
		cfg.UDPListen = *udpAddr
	}
	if flagSet(fs, "stun-alt") {
		cfg.StunAltListen = *stunAlt
	}
	if flagSet(fs, "http") {
		cfg.HTTPListen = *httpAddr
	}
	if flagSet(fs, "db") {
		cfg.DatabasePath = *dbPath
	}
	if flagSet(fs, "psk") {
		cfg.PSK = *psk
	}
	if flagSet(fs, "admin-password") {
		cfg.AdminPassword = *adminPassword
	}
	if flagSet(fs, "admin-password-hash") {
		cfg.AdminPasswordHash = *adminHash
	}
	if flagSet(fs, "peer-ttl") {
		cfg.PeerTTL = *peerTTL
	}
	if flagSet(fs, "pair-ttl") {
		cfg.PairTTL = *pairTTL
	}
	if flagSet(fs, "relay-idle-timeout") {
		cfg.RelayIdleTimeout = *relayIdle
	}
	if flagSet(fs, "allow-relay") {
		cfg.AllowRelay = *allowRelay
	}
	if flagSet(fs, "allow-legacy") {
		cfg.AllowLegacy = *allowLegacy
	}

	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var codec *secure.Codec
	if cfg.PSK != "" {
		codec, err = secure.NewCodec(cfg.PSK)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		log.Printf("WARN product PSK is empty; secure UDP frames and agent auth are disabled")
		cfg.AllowLegacy = true
	}

	a := &app{
		cfg:       cfg,
		db:        db,
		codec:     codec,
		startTime: time.Now(),
		peers:     map[string]map[string]*peer{},
		pairByID:  map[string]int64{},
		sessions:  map[string]time.Time{},
	}
	if err := a.ensureAdminPassword(); err != nil {
		log.Fatal(err)
	}
	if err := a.applyStoredSettings(); err != nil {
		log.Fatal(err)
	}

	go a.cleanupLoop()
	go a.runHTTP()
	go a.runStunAlt()
	a.runUDP()
}

func (a *app) runStunAlt() {
	uaddr, err := net.ResolveUDPAddr("udp", a.cfg.StunAltListen)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", uaddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Printf("rendezvous server (STUN-ALT) listening on %s", a.cfg.StunAltListen)

	buf := make([]byte, 4096)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("stun-alt read error: %v", err)
			continue
		}
		kind, payload, ok := a.openPacket(buf[:n])
		if !ok || kind != secure.KindControl {
			continue
		}
		msg, err := protocol.Decode(payload)
		if err != nil || msg.Type != protocol.MsgStunReq {
			continue
		}
		a.writeControl(conn, src, &protocol.Message{Type: protocol.MsgStunResp, Addr: src.String()})
	}
}

func (a *app) runUDP() {
	uaddr, err := net.ResolveUDPAddr("udp", a.cfg.UDPListen)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", uaddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Printf("rendezvous server (UDP) listening on %s", a.cfg.UDPListen)

	buf := make([]byte, 65535)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("read error: %v", err)
			continue
		}
		data := append([]byte(nil), buf[:n]...)
		if a.handleRelay(conn, src, data) {
			continue
		}
		kind, payload, ok := a.openPacket(data)
		if !ok || kind != secure.KindControl {
			continue
		}
		msg, err := protocol.Decode(payload)
		if err != nil {
			log.Printf("bad packet from %s: %v", src, err)
			continue
		}
		switch msg.Type {
		case protocol.MsgStunReq:
			a.writeControl(conn, src, &protocol.Message{Type: protocol.MsgStunResp, Addr: src.String()})
		case protocol.MsgRegister:
			a.handleRegister(conn, src, msg)
		}
	}
}

func (a *app) handleRelay(conn *net.UDPConn, src *net.UDPAddr, data []byte) bool {
	if !a.currentAllowRelay() {
		return false
	}
	isKCP := true
	if secure.IsFrame(data) && a.codec != nil {
		kind, _, err := a.codec.Open(data)
		if err != nil {
			return true
		}
		isKCP = kind == secure.KindKCP
	} else if len(data) > 0 && data[0] == '{' {
		isKCP = false
	}
	if !isKCP {
		return false
	}
	if dstRaw, ok := a.pairs.Load(src.String()); ok {
		route := dstRaw.(pairRoute)
		if _, err := conn.WriteToUDP(data, route.dst); err == nil {
			a.totalRelayed.Add(uint64(len(data)))
			_ = a.db.TouchSession(rctx(), route.sessionID, int64(len(data)))
			route.lastSeen = time.Now()
			a.pairs.Store(src.String(), route)
		}
		return true
	}
	return false
}

func (a *app) openPacket(data []byte) (byte, []byte, bool) {
	if secure.IsFrame(data) {
		if a.codec == nil {
			return 0, nil, false
		}
		kind, payload, err := a.codec.Open(data)
		if err != nil {
			log.Printf("secure frame open failed: %v", err)
			return 0, nil, false
		}
		return kind, payload, true
	}
	if a.currentAllowLegacy() && len(data) > 0 && data[0] == '{' {
		return secure.KindControl, data, true
	}
	return 0, nil, false
}

func (a *app) writeControl(conn *net.UDPConn, dst *net.UDPAddr, msg *protocol.Message) {
	b, _ := protocol.Encode(msg)
	if a.codec != nil {
		var err error
		b, err = a.codec.Seal(secure.KindControl, b)
		if err != nil {
			log.Printf("seal control failed: %v", err)
			return
		}
	}
	if _, err := conn.WriteToUDP(b, dst); err != nil {
		log.Printf("send control to %s failed: %v", dst, err)
	}
}

func (a *app) handleRegister(conn *net.UDPConn, src *net.UDPAddr, msg *protocol.Message) {
	log.Printf("register: id=%s want=%s from=%s upnp=%q", msg.From, msg.Peer, src, msg.UpnpAddr)
	a.totalRegister.Add(1)
	name := msg.Name
	if name == "" {
		name = msg.From
	}
	_ = a.db.UpsertDevice(rctx(), msg.From, name, src.String(), msg.UpnpAddr, msg.Peer, true)

	a.mu.Lock()
	defer a.mu.Unlock()
	byWant, ok := a.peers[msg.From]
	if !ok {
		byWant = map[string]*peer{}
		a.peers[msg.From] = byWant
	}
	if old, ok := byWant[msg.Peer]; ok && old.addr.String() != src.String() {
		// 同一 (from, want) 换 socket，旧路由作废
		a.pairs.Delete(old.addr.String())
	}
	self := &peer{id: msg.From, addr: cloneUDP(src), upnpAddr: msg.UpnpAddr, want: msg.Peer, lastSeen: time.Now()}
	byWant[msg.Peer] = self

	other, ok := a.lookupPeer(msg.Peer, msg.From)
	if !ok {
		log.Printf("  waiting for peer %s to register want=%s...", msg.Peer, msg.From)
		return
	}
	a.sendPeer(conn, self, other)
	a.sendPeer(conn, other, self)
	sessionID := a.ensurePairSession(self.id, other.id, "pending")
	self.sessionID = sessionID
	other.sessionID = sessionID
	a.pairs.Store(self.addr.String(), pairRoute{dst: cloneUDP(other.addr), lastSeen: time.Now(), sessionID: sessionID})
	a.pairs.Store(other.addr.String(), pairRoute{dst: cloneUDP(self.addr), lastSeen: time.Now(), sessionID: sessionID})
	log.Printf("paired: %s(%s) <-> %s(%s)", self.id, self.addr, other.id, other.addr)
	a.totalPaired.Add(1)
}

// lookupPeer 查 (from, want) 槽。调用方需持有 a.mu。
func (a *app) lookupPeer(from, want string) (*peer, bool) {
	byWant, ok := a.peers[from]
	if !ok {
		return nil, false
	}
	p, ok := byWant[want]
	return p, ok
}

func (a *app) sendPeer(conn *net.UDPConn, to, about *peer) {
	a.writeControl(conn, to.addr, &protocol.Message{
		Type:     protocol.MsgPeerInfo,
		Peer:     about.id,
		Addr:     about.addr.String(),
		UpnpAddr: about.upnpAddr,
	})
}

func (a *app) ensurePairSession(aID, bID, path string) int64 {
	key := pairKey(aID, bID)
	if id, ok := a.pairByID[key]; ok {
		return id
	}
	id, err := a.db.StartSession(rctx(), aID, bID, path)
	if err != nil {
		log.Printf("start session failed: %v", err)
		return 0
	}
	a.pairByID[key] = id
	return id
}

func (a *app) cleanupLoop() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		peerTTL := a.currentPeerTTL()
		pairTTL := a.currentPairTTL()
		relayIdle := a.currentRelayIdleTimeout()
		a.mu.Lock()
		for from, byWant := range a.peers {
			for want, p := range byWant {
				if now.Sub(p.lastSeen) > peerTTL {
					delete(byWant, want)
					a.pairs.Delete(p.addr.String())
				}
			}
			if len(byWant) == 0 {
				delete(a.peers, from)
			}
		}
		a.mu.Unlock()
		a.pairs.Range(func(key, value any) bool {
			route := value.(pairRoute)
			if now.Sub(route.lastSeen) > pairTTL {
				a.pairs.Delete(key)
			}
			return true
		})
		_ = a.db.MarkOfflineBefore(rctx(), now.Add(-peerTTL))
		_ = a.db.EndIdleSessions(rctx(), now.Add(-relayIdle))
	}
}

func (a *app) runHTTP() {
	mux := a.httpMux()
	log.Printf("rendezvous server (HTTP) listening on %s", a.cfg.HTTPListen)
	if err := http.ListenAndServe(a.cfg.HTTPListen, mux); err != nil {
		log.Fatalf("http listen failed: %v", err)
	}
}

func (a *app) httpMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/peers", a.handlePeers)
	mux.HandleFunc("/api/login", a.handleLogin)
	mux.HandleFunc("/api/logout", a.requireWeb(a.handleLogout))
	mux.HandleFunc("/api/me", a.requireWeb(a.handleMe))
	mux.HandleFunc("/api/devices", a.requireWeb(a.handleDevices))
	mux.HandleFunc("/api/devices/", a.requireWeb(a.handleDevice))
	mux.HandleFunc("/api/forwards", a.requireWeb(a.handleForwards))
	mux.HandleFunc("/api/forwards/", a.requireWeb(a.handleForward))
	mux.HandleFunc("/api/sessions", a.requireWeb(a.handleSessions))
	mux.HandleFunc("/api/tunnel-states", a.requireWeb(a.handleTunnelStates))
	mux.HandleFunc("/api/metrics", a.requireWeb(a.handleMetrics))
	mux.HandleFunc("/api/settings", a.requireWeb(a.handleSettings))
	mux.HandleFunc("/api/admin/password", a.requireWeb(a.handleChangePassword))
	mux.HandleFunc("/api/agent/register", a.requireAgent(a.handleAgentRegister))
	mux.HandleFunc("/api/agent/heartbeat", a.requireAgent(a.handleAgentHeartbeat))
	mux.HandleFunc("/api/agent/tunnel-status", a.requireAgent(a.handleAgentTunnelStatus))
	mux.HandleFunc("/api/agent/bootstrap", a.requireAgent(a.handleAgentBootstrap))
	mux.HandleFunc("/api/agent/rules", a.requireAgent(a.handleAgentRules))
	mux.Handle("/", a.staticHandler())
	return mux
}

func (a *app) staticHandler() http.Handler {
	sub, _ := fs.Sub(webFS, "web")
	return http.FileServer(http.FS(sub))
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	peerCount := 0
	for _, byWant := range a.peers {
		peerCount += len(byWant)
	}
	a.mu.Unlock()
	metrics, _ := a.db.Metrics(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"status":              "ok",
		"uptime_seconds":      int64(time.Since(a.startTime).Seconds()),
		"total_register":      a.totalRegister.Load(),
		"total_paired":        a.totalPaired.Load(),
		"total_relayed_bytes": a.totalRelayed.Load(),
		"current_peers":       peerCount,
		"metrics":             metrics,
		"server_time":         time.Now().Format(time.RFC3339),
	})
}

func (a *app) handlePeers(w http.ResponseWriter, r *http.Request) {
	devices, err := a.db.ListDevices(r.Context())
	writeJSONOrError(w, devices, err)
}

func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	hash, _ := a.db.GetMeta(r.Context(), "admin_password_hash")
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token := randomHex(32)
	a.authMu.Lock()
	a.sessions[token] = time.Now().Add(12 * time.Hour)
	a.authMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "udp_tunnel_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(12 * time.Hour)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("udp_tunnel_session"); err == nil {
		a.authMu.Lock()
		delete(a.sessions, c.Value)
		a.authMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "udp_tunnel_session", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"user": "admin"})
}

func (a *app) handleDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := a.enrichedDevices(r.Context())
	writeJSONOrError(w, devices, err)
}

func (a *app) handleDevice(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	switch r.Method {
	case http.MethodGet:
		d, err := a.db.GetDevice(r.Context(), id)
		writeJSONOrError(w, d, err)
	case http.MethodPatch:
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
			return
		}
		err := a.db.SetDeviceEnabled(r.Context(), id, req.Enabled)
		if err == nil {
			_ = a.db.Audit(r.Context(), "device_set_enabled", fmt.Sprintf("%s enabled=%v", id, req.Enabled))
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	case http.MethodDelete:
		n, err := a.db.EnabledRuleReferenceCount(r.Context(), id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		if n > 0 {
			writeJSONOrError(w, nil, badRequest("device_in_use", "device is still referenced by enabled rules"))
			return
		}
		err = a.db.DeleteDevice(r.Context(), id)
		if err == nil {
			_ = a.db.Audit(r.Context(), "device_delete", id)
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleForwards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules, err := a.enrichedRules(r.Context())
		writeJSONOrError(w, rules, err)
	case http.MethodPost:
		rule, err := decodeRule(r)
		if err == nil {
			err = normalizeRuleValidationError(rule.Validate(), rule)
		}
		if err == nil {
			err = a.validateRule(r.Context(), rule, 0)
		}
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		rule, err = a.db.CreateRule(r.Context(), rule)
		if err == nil {
			_ = a.db.Audit(r.Context(), "rule_create", fmt.Sprintf("#%d %s->%s:%d", rule.ID, rule.SourceID, rule.TargetID, rule.LocalPort))
		}
		writeJSONOrError(w, rule, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleForward(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/forwards/"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		rule, err := decodeRule(r)
		if err == nil {
			err = normalizeRuleValidationError(rule.Validate(), rule)
		}
		if err == nil {
			err = a.validateRule(r.Context(), rule, id)
		}
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		err = a.db.UpdateRule(r.Context(), id, rule)
		if err == nil {
			_ = a.db.Audit(r.Context(), "rule_update", fmt.Sprintf("#%d %s->%s:%d", id, rule.SourceID, rule.TargetID, rule.LocalPort))
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	case http.MethodDelete:
		err := a.db.DeleteRule(r.Context(), id)
		if err == nil {
			_ = a.db.Audit(r.Context(), "rule_delete", fmt.Sprintf("#%d", id))
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := a.db.ListSessions(r.Context())
	writeJSONOrError(w, sessions, err)
}

func (a *app) handleTunnelStates(w http.ResponseWriter, r *http.Request) {
	states, err := a.db.ListTunnelStates(r.Context())
	writeJSONOrError(w, states, err)
}

func (a *app) handleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := a.db.Metrics(r.Context())
	writeJSONOrError(w, metrics, err)
}

func (a *app) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.cfgMu.RLock()
		resp := map[string]any{
			"udp_listen":           a.cfg.UDPListen,
			"stun_alt_listen":      a.cfg.StunAltListen,
			"http_listen":          a.cfg.HTTPListen,
			"database_path":        a.cfg.DatabasePath,
			"psk_configured":       a.cfg.PSK != "",
			"peer_ttl":             a.cfg.PeerTTL.String(),
			"pair_ttl":             a.cfg.PairTTL.String(),
			"relay_idle_timeout":   a.cfg.RelayIdleTimeout.String(),
			"allow_relay":          a.cfg.AllowRelay,
			"allow_legacy":         a.cfg.AllowLegacy,
			"client_no_upnp":       a.cfg.ClientNoUPnP,
			"client_upnp_timeout":  a.cfg.ClientUPnPTimeout.String(),
			"client_log_level":     a.cfg.ClientLogLevel,
			"client_tray_enabled":  a.cfg.ClientTrayEnabled,
			"client_punch_timeout": a.cfg.ClientPunchTimeout.String(),
			"client_force_relay":   a.cfg.ClientForceRelay,
			"client_allow_legacy":  a.cfg.ClientAllowLegacy,
			"restart_only_fields":  []string{"udp_listen", "stun_alt_listen", "http_listen", "database_path", "psk"},
		}
		a.cfgMu.RUnlock()
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPatch:
		var req struct {
			PeerTTL            string `json:"peer_ttl"`
			PairTTL            string `json:"pair_ttl"`
			RelayIdleTimeout   string `json:"relay_idle_timeout"`
			AllowRelay         bool   `json:"allow_relay"`
			AllowLegacy        bool   `json:"allow_legacy"`
			ClientNoUPnP       bool   `json:"client_no_upnp"`
			ClientUPnPTimeout  string `json:"client_upnp_timeout"`
			ClientLogLevel     string `json:"client_log_level"`
			ClientTrayEnabled  bool   `json:"client_tray_enabled"`
			ClientPunchTimeout string `json:"client_punch_timeout"`
			ClientForceRelay   bool   `json:"client_force_relay"`
			ClientAllowLegacy  bool   `json:"client_allow_legacy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		peerTTL, err := time.ParseDuration(req.PeerTTL)
		if err != nil {
			http.Error(w, "bad peer_ttl", http.StatusBadRequest)
			return
		}
		pairTTL, err := time.ParseDuration(req.PairTTL)
		if err != nil {
			http.Error(w, "bad pair_ttl", http.StatusBadRequest)
			return
		}
		relayIdle, err := time.ParseDuration(req.RelayIdleTimeout)
		if err != nil {
			http.Error(w, "bad relay_idle_timeout", http.StatusBadRequest)
			return
		}
		if peerTTL < 10*time.Second || pairTTL < 10*time.Second || relayIdle < 10*time.Second {
			http.Error(w, "durations must be at least 10s", http.StatusBadRequest)
			return
		}
		clientUPnPTimeout, err := time.ParseDuration(req.ClientUPnPTimeout)
		if err != nil {
			http.Error(w, "bad client_upnp_timeout", http.StatusBadRequest)
			return
		}
		clientPunchTimeout, err := time.ParseDuration(req.ClientPunchTimeout)
		if err != nil {
			http.Error(w, "bad client_punch_timeout", http.StatusBadRequest)
			return
		}
		if clientUPnPTimeout < time.Second || clientPunchTimeout < time.Second {
			http.Error(w, "client durations must be at least 1s", http.StatusBadRequest)
			return
		}
		a.cfgMu.Lock()
		a.cfg.PeerTTL = peerTTL
		a.cfg.PairTTL = pairTTL
		a.cfg.RelayIdleTimeout = relayIdle
		a.cfg.AllowRelay = req.AllowRelay
		a.cfg.AllowLegacy = req.AllowLegacy
		a.cfg.ClientNoUPnP = req.ClientNoUPnP
		a.cfg.ClientUPnPTimeout = clientUPnPTimeout
		a.cfg.ClientLogLevel = req.ClientLogLevel
		a.cfg.ClientTrayEnabled = req.ClientTrayEnabled
		a.cfg.ClientPunchTimeout = clientPunchTimeout
		a.cfg.ClientForceRelay = req.ClientForceRelay
		a.cfg.ClientAllowLegacy = req.ClientAllowLegacy
		a.cfgMu.Unlock()
		_ = a.db.PutMeta(r.Context(), "setting_peer_ttl", peerTTL.String())
		_ = a.db.PutMeta(r.Context(), "setting_pair_ttl", pairTTL.String())
		_ = a.db.PutMeta(r.Context(), "setting_relay_idle_timeout", relayIdle.String())
		_ = a.db.PutMeta(r.Context(), "setting_allow_relay", strconv.FormatBool(req.AllowRelay))
		_ = a.db.PutMeta(r.Context(), "setting_allow_legacy", strconv.FormatBool(req.AllowLegacy))
		_ = a.db.PutMeta(r.Context(), "setting_client_no_upnp", strconv.FormatBool(req.ClientNoUPnP))
		_ = a.db.PutMeta(r.Context(), "setting_client_upnp_timeout", clientUPnPTimeout.String())
		_ = a.db.PutMeta(r.Context(), "setting_client_log_level", req.ClientLogLevel)
		_ = a.db.PutMeta(r.Context(), "setting_client_tray_enabled", strconv.FormatBool(req.ClientTrayEnabled))
		_ = a.db.PutMeta(r.Context(), "setting_client_punch_timeout", clientPunchTimeout.String())
		_ = a.db.PutMeta(r.Context(), "setting_client_force_relay", strconv.FormatBool(req.ClientForceRelay))
		_ = a.db.PutMeta(r.Context(), "setting_client_allow_legacy", strconv.FormatBool(req.ClientAllowLegacy))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(req.NewPassword) < 8 {
		http.Error(w, "new password must be at least 8 chars", http.StatusBadRequest)
		return
	}
	hash, _ := a.db.GetMeta(r.Context(), "admin_password_hash")
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword)) != nil {
		http.Error(w, "current password is wrong", http.StatusUnauthorized)
		return
	}
	next, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.db.PutMeta(r.Context(), "admin_password_hash", string(next)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string              `json:"device_id"`
		Name     string              `json:"name"`
		Addr     string              `json:"addr"`
		UpnpAddr string              `json:"upnp_addr"`
		NATType  string              `json:"nat_type"`
		Tunnels  []agentTunnelReport `json:"tunnels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := a.ensureAgentDeviceAllowed(r.Context(), req.DeviceID); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	addr := req.Addr
	if addr == "" {
		addr = requestAddr(r)
	}
	name := req.Name
	if name == "" {
		name = req.DeviceID
	}
	err := a.db.UpsertDevice(r.Context(), req.DeviceID, name, addr, req.UpnpAddr, "", a.agentOnline(req.Tunnels))
	if err == nil {
		err = a.putTunnelReports(r.Context(), req.DeviceID, req.Tunnels)
	}
	writeJSONOrError(w, map[string]any{"ok": true}, err)
}

func (a *app) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string              `json:"device_id"`
		Name     string              `json:"name"`
		Addr     string              `json:"addr"`
		UpnpAddr string              `json:"upnp_addr"`
		NATType  string              `json:"nat_type"`
		Tunnels  []agentTunnelReport `json:"tunnels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := a.ensureAgentDeviceAllowed(r.Context(), req.DeviceID); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	addr := req.Addr
	if addr == "" {
		addr = requestAddr(r)
	}
	name := req.Name
	if name == "" {
		name = req.DeviceID
	}
	err := a.db.UpsertDevice(r.Context(), req.DeviceID, name, addr, req.UpnpAddr, "", a.agentOnline(req.Tunnels))
	if err == nil {
		err = a.putTunnelReports(r.Context(), req.DeviceID, req.Tunnels)
	}
	writeJSONOrError(w, map[string]any{"ok": true}, err)
}

func (a *app) handleAgentTunnelStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID   string `json:"device_id"`
		Peer       string `json:"peer"`
		State      string `json:"state"`
		Via        string `json:"via"`
		NATType    string `json:"nat_type"`
		Addr       string `json:"addr"`
		UpnpAddr   string `json:"upnp_addr"`
		PublicAddr string `json:"public_addr"`
		ConvID     int64  `json:"conv_id"`
		RTTMs      int    `json:"rtt_ms"`
		LastError  string `json:"last_error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" || req.Peer == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := a.ensureAgentDeviceAllowed(r.Context(), req.DeviceID); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	addr := req.Addr
	if addr == "" {
		addr = requestAddr(r)
	}
	online := req.State == "connecting" || req.State == "p2p" || req.State == "relay"
	err := a.db.UpsertDevice(r.Context(), req.DeviceID, req.DeviceID, addr, req.UpnpAddr, req.Peer, online)
	if err == nil {
		err = a.db.PutTunnelState(r.Context(), store.TunnelState{
			DeviceID:   req.DeviceID,
			PeerID:     req.Peer,
			State:      req.State,
			Via:        req.Via,
			NATType:    req.NATType,
			PublicAddr: req.PublicAddr,
			ConvID:     req.ConvID,
			RTTMs:      req.RTTMs,
			LastError:  req.LastError,
		})
	}
	if err == nil && (req.State == "p2p" || req.State == "relay") && req.Via != "" {
		err = a.db.UpdateSessionPathForPair(r.Context(), req.DeviceID, req.Peer, req.Via)
	}
	writeJSONOrError(w, map[string]any{"ok": true}, err)
}

func (a *app) handleAgentBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := a.ensureAgentDeviceAllowed(r.Context(), req.DeviceID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeJSONOrError(w, nil, err)
		return
	}
	name := strings.TrimSpace(req.DeviceName)
	if name == "" {
		name = req.DeviceID
	}
	resp := a.bootstrapConfig(r, req.DeviceID, name)
	err := a.db.UpsertDevice(r.Context(), req.DeviceID, name, requestAddr(r), "", "", false)
	writeJSONOrError(w, resp, err)
}

func (a *app) handleAgentRules(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if deviceID == "" {
		http.Error(w, "device_id required", http.StatusBadRequest)
		return
	}
	if err := a.ensureAgentDeviceAllowed(r.Context(), deviceID); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	rules, err := a.db.RulesForDevice(r.Context(), deviceID)
	writeJSONOrError(w, rules, err)
}

func (a *app) requireWeb(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("udp_tunnel_session")
		if err != nil || !a.validSession(c.Value) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *app) requireAgent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.PSK != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-UDP-Tunnel-PSK")), []byte(a.cfg.PSK)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *app) validSession(token string) bool {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	exp, ok := a.sessions[token]
	if !ok || time.Now().After(exp) {
		delete(a.sessions, token)
		return false
	}
	return true
}

func (a *app) ensureAdminPassword() error {
	hash := a.cfg.AdminPasswordHash
	if hash == "" {
		if existing, _ := a.db.GetMeta(rctx(), "admin_password_hash"); existing != "" {
			return nil
		}
		pass := a.cfg.AdminPassword
		if pass == "" {
			pass = "admin"
			log.Printf("WARN admin password defaulting to %q; change it in server.json or with -admin-password", pass)
		}
		b, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		hash = string(b)
	}
	return a.db.PutMeta(rctx(), "admin_password_hash", hash)
}

func (a *app) applyStoredSettings() error {
	ctx := rctx()
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	if v, err := a.db.GetMeta(ctx, "setting_peer_ttl"); err != nil {
		return err
	} else if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		a.cfg.PeerTTL = d
	}
	if v, err := a.db.GetMeta(ctx, "setting_pair_ttl"); err != nil {
		return err
	} else if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		a.cfg.PairTTL = d
	}
	if v, err := a.db.GetMeta(ctx, "setting_relay_idle_timeout"); err != nil {
		return err
	} else if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		a.cfg.RelayIdleTimeout = d
	}
	if v, err := a.db.GetMeta(ctx, "setting_allow_relay"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.AllowRelay = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_allow_legacy"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.AllowLegacy = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_no_upnp"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.ClientNoUPnP = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_upnp_timeout"); err != nil {
		return err
	} else if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		a.cfg.ClientUPnPTimeout = d
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_log_level"); err != nil {
		return err
	} else if v != "" {
		a.cfg.ClientLogLevel = v
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_tray_enabled"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.ClientTrayEnabled = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_punch_timeout"); err != nil {
		return err
	} else if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		a.cfg.ClientPunchTimeout = d
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_force_relay"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.ClientForceRelay = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_allow_legacy"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.ClientAllowLegacy = b
	}
	return nil
}

func (a *app) currentPeerTTL() time.Duration {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.PeerTTL
}

func (a *app) currentPairTTL() time.Duration {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.PairTTL
}

func (a *app) currentRelayIdleTimeout() time.Duration {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.RelayIdleTimeout
}

func (a *app) currentAllowRelay() bool {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.AllowRelay
}

func (a *app) currentAllowLegacy() bool {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.AllowLegacy
}

func decodeRule(r *http.Request) (store.ForwardRule, error) {
	var rule store.ForwardRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		return rule, err
	}
	return rule, nil
}

func normalizeRuleValidationError(err error, rule store.ForwardRule) error {
	if err == nil {
		return nil
	}
	if rule.SourceID == rule.TargetID && strings.TrimSpace(rule.SourceID) != "" {
		return badRequest("same_device_forbidden", "source_id and target_id must differ")
	}
	return badRequest("bad_rule", err.Error())
}

func (a *app) validateRule(ctx context.Context, rule store.ForwardRule, excludeID int64) error {
	if strings.TrimSpace(rule.SourceID) == "" || strings.TrimSpace(rule.TargetID) == "" {
		return badRequest("device_not_found", "source_id and target_id are required")
	}
	source, err := a.db.GetDevice(ctx, rule.SourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return badRequest("device_not_found", "source device not found")
		}
		return err
	}
	target, err := a.db.GetDevice(ctx, rule.TargetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return badRequest("device_not_found", "target device not found")
		}
		return err
	}
	if source.ID == target.ID {
		return badRequest("same_device_forbidden", "source_id and target_id must differ")
	}
	if !source.Enabled || !target.Enabled {
		return badRequest("device_disabled", "source or target device is disabled")
	}
	if !rule.Enabled {
		return nil
	}
	conflict, err := a.db.LocalPortConflict(ctx, rule.SourceID, rule.LocalPort, excludeID)
	if err != nil {
		return err
	}
	if conflict {
		return badRequest("local_port_conflict", "same source_id cannot reuse local_port across enabled rules")
	}
	return nil
}

func (a *app) agentOnline(tunnels []agentTunnelReport) bool {
	if len(tunnels) == 0 {
		return true
	}
	for _, t := range tunnels {
		switch t.State {
		case "connecting", "p2p", "relay":
			return true
		}
	}
	return false
}

func (a *app) putTunnelReports(ctx context.Context, deviceID string, tunnels []agentTunnelReport) error {
	for _, t := range tunnels {
		if t.Peer == "" {
			continue
		}
		if err := a.db.PutTunnelState(ctx, store.TunnelState{
			DeviceID:   deviceID,
			PeerID:     t.Peer,
			State:      t.State,
			Via:        t.Via,
			NATType:    t.NATType,
			PublicAddr: t.PublicAddr,
			ConvID:     t.ConvID,
			RTTMs:      t.RTTMs,
			LastError:  t.LastError,
		}); err != nil {
			return err
		}
		if (t.State == "p2p" || t.State == "relay") && t.Via != "" {
			if err := a.db.UpdateSessionPathForPair(ctx, deviceID, t.Peer, t.Via); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *app) ensureAgentDeviceAllowed(ctx context.Context, deviceID string) error {
	d, err := a.db.GetDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	if !d.Enabled {
		return badRequest("device_disabled", "device is disabled")
	}
	return nil
}

func (a *app) enrichedDevices(ctx context.Context) ([]store.Device, error) {
	devices, err := a.db.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := a.db.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	states, err := a.db.ListTunnelStates(ctx)
	if err != nil {
		return nil, err
	}
	stateMap := latestStateByPair(states)
	deviceErrs := map[string]string{}
	for _, st := range states {
		if st.LastError != "" && deviceErrs[st.DeviceID] == "" {
			deviceErrs[st.DeviceID] = st.LastError
		}
	}
	ruleCount := map[string]int{}
	healthy := map[string]bool{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		ruleCount[r.SourceID]++
		ruleCount[r.TargetID]++
		if st, ok := stateMap[pairStateKey(r.SourceID, r.TargetID)]; ok && (st.State == "p2p" || st.State == "relay") {
			healthy[r.SourceID] = true
			healthy[r.TargetID] = true
		}
	}
	for i := range devices {
		switch {
		case ruleCount[devices[i].ID] == 0:
			devices[i].HealthSummary = "无规则"
		case healthy[devices[i].ID]:
			devices[i].HealthSummary = "至少一条隧道正常"
		default:
			devices[i].HealthSummary = "有规则但未建链"
		}
		devices[i].LastError = deviceErrs[devices[i].ID]
	}
	return devices, nil
}

func (a *app) enrichedRules(ctx context.Context) ([]store.ForwardRule, error) {
	rules, err := a.db.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := a.db.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	states, err := a.db.ListTunnelStates(ctx)
	if err != nil {
		return nil, err
	}
	deviceMap := map[string]store.Device{}
	for _, d := range devices {
		deviceMap[d.ID] = d
	}
	stateMap := latestStateByPair(states)
	for i := range rules {
		r := &rules[i]
		if !r.Enabled {
			r.RuntimeState = "disabled"
			r.LastError = ""
			r.LastUpdatedAt = r.UpdatedAt
			continue
		}
		src, srcOK := deviceMap[r.SourceID]
		dst, dstOK := deviceMap[r.TargetID]
		switch {
		case !srcOK || !dstOK:
			r.RuntimeState = "down"
			r.LastError = "device_not_found"
			r.LastUpdatedAt = r.UpdatedAt
		case !src.Enabled || !dst.Enabled:
			r.RuntimeState = "down"
			r.LastError = "device_disabled"
			r.LastUpdatedAt = r.UpdatedAt
		default:
			st, ok := stateMap[pairStateKey(r.SourceID, r.TargetID)]
			if !ok {
				r.RuntimeState = "down"
				r.LastError = "session_not_established"
				r.LastUpdatedAt = r.UpdatedAt
				continue
			}
			r.RuntimeState = normalizeRuntimeState(st.State, st.Via)
			r.LastError = st.LastError
			r.LastUpdatedAt = st.UpdatedAt
		}
	}
	return rules, nil
}

func latestStateByPair(states []store.TunnelState) map[string]store.TunnelState {
	out := map[string]store.TunnelState{}
	for _, st := range states {
		key := pairStateKey(st.DeviceID, st.PeerID)
		if _, ok := out[key]; !ok {
			out[key] = st
		}
	}
	return out
}

func pairStateKey(a, b string) string {
	if a <= b {
		return a + "\x00" + b
	}
	return b + "\x00" + a
}

func normalizeRuntimeState(state, via string) string {
	switch state {
	case "p2p", "relay", "connecting", "down", "disabled":
		return state
	case "connected":
		if via == "relay" {
			return "relay"
		}
		return "p2p"
	case "stopped", "":
		return "down"
	default:
		if via == "relay" {
			return "relay"
		}
		return state
	}
}

func badRequest(code, message string) error {
	return &apiError{Status: http.StatusBadRequest, Code: code, Message: message}
}

func writeJSONOrError(w http.ResponseWriter, v any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			writeJSON(w, apiErr.Status, apiErr)
			return
		}
		if errors.Is(err, sqlErrNoRows()) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"code": "internal_error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func requestAddr(r *http.Request) string {
	host, port, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return net.JoinHostPort(host, port)
}

func (a *app) bootstrapConfig(r *http.Request, deviceID, deviceName string) agentBootstrapResponse {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return agentBootstrapResponse{
		DeviceID:     deviceID,
		DeviceName:   deviceName,
		Server:       externalUDPAddr(r, a.cfg.UDPListen),
		ServerHTTP:   requestBaseURL(r),
		STUNAltPort:  portFromAddr(a.cfg.StunAltListen, 7002),
		NoUPnP:       a.cfg.ClientNoUPnP,
		UPnPTimeout:  a.cfg.ClientUPnPTimeout.String(),
		LogLevel:     a.cfg.ClientLogLevel,
		TrayEnabled:  a.cfg.ClientTrayEnabled,
		PunchTimeout: a.cfg.ClientPunchTimeout.String(),
		ForceRelay:   a.cfg.ClientForceRelay,
		AllowLegacy:  a.cfg.ClientAllowLegacy,
	}
}

func externalUDPAddr(r *http.Request, udpListen string) string {
	host, _, _ := net.SplitHostPort(r.Host)
	if host == "" {
		host = r.Host
	}
	udpHost, udpPort, err := net.SplitHostPort(udpListen)
	if err != nil {
		return net.JoinHostPort(host, "7000")
	}
	switch udpHost {
	case "", "0.0.0.0", "::":
		udpHost = host
	}
	return net.JoinHostPort(udpHost, udpPort)
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); xf != "" {
		scheme = xf
	}
	return scheme + "://" + r.Host
}

func portFromAddr(addr string, fallback int) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return fallback
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return fallback
	}
	return n
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func pairKey(a, b string) string {
	if a < b {
		return a + "\x00" + b
	}
	return b + "\x00" + a
}

func cloneUDP(a *net.UDPAddr) *net.UDPAddr {
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

func rctx() context.Context {
	return context.Background()
}

func sqlErrNoRows() error {
	return sql.ErrNoRows
}

func flagSet(fs *flag.FlagSet, name string) bool {
	seen := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}
