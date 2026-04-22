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
	peers    map[string]*peer
	pairByID map[string]int64
	pairs    sync.Map // src address string -> pairRoute

	authMu   sync.Mutex
	sessions map[string]time.Time
	cfgMu    sync.RWMutex
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
		peers:     map[string]*peer{},
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
	_ = a.db.UpsertDevice(rctx(), msg.From, src.String(), msg.UpnpAddr, msg.Peer, true)

	a.mu.Lock()
	defer a.mu.Unlock()
	if old, ok := a.peers[msg.From]; ok && old.addr.String() != src.String() {
		a.pairs.Delete(old.addr.String())
	}
	a.peers[msg.From] = &peer{id: msg.From, addr: cloneUDP(src), upnpAddr: msg.UpnpAddr, want: msg.Peer, lastSeen: time.Now()}

	self := a.peers[msg.From]
	other, ok := a.peers[msg.Peer]
	if !ok || other.want != msg.From {
		log.Printf("  waiting for peer %s to register...", msg.Peer)
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
		for id, p := range a.peers {
			if now.Sub(p.lastSeen) > peerTTL {
				delete(a.peers, id)
				a.pairs.Delete(p.addr.String())
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
	mux.HandleFunc("/api/metrics", a.requireWeb(a.handleMetrics))
	mux.HandleFunc("/api/settings", a.requireWeb(a.handleSettings))
	mux.HandleFunc("/api/admin/password", a.requireWeb(a.handleChangePassword))
	mux.HandleFunc("/api/agent/register", a.requireAgent(a.handleAgentRegister))
	mux.HandleFunc("/api/agent/heartbeat", a.requireAgent(a.handleAgentHeartbeat))
	mux.HandleFunc("/api/agent/rules", a.requireAgent(a.handleAgentRules))
	mux.Handle("/", a.staticHandler())
	log.Printf("rendezvous server (HTTP) listening on %s", a.cfg.HTTPListen)
	if err := http.ListenAndServe(a.cfg.HTTPListen, mux); err != nil {
		log.Fatalf("http listen failed: %v", err)
	}
}

func (a *app) staticHandler() http.Handler {
	sub, _ := fs.Sub(webFS, "web")
	return http.FileServer(http.FS(sub))
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	peerCount := len(a.peers)
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
	devices, err := a.db.ListDevices(r.Context())
	writeJSONOrError(w, devices, err)
}

func (a *app) handleDevice(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	d, err := a.db.GetDevice(r.Context(), id)
	writeJSONOrError(w, d, err)
}

func (a *app) handleForwards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules, err := a.db.ListRules(r.Context())
		writeJSONOrError(w, rules, err)
	case http.MethodPost:
		rule, err := decodeRule(r)
		if err == nil {
			err = rule.Validate()
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rule, err = a.db.CreateRule(r.Context(), rule)
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
			err = rule.Validate()
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOrError(w, map[string]any{"ok": true}, a.db.UpdateRule(r.Context(), id, rule))
	case http.MethodDelete:
		writeJSONOrError(w, map[string]any{"ok": true}, a.db.DeleteRule(r.Context(), id))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := a.db.ListSessions(r.Context())
	writeJSONOrError(w, sessions, err)
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
			"udp_listen":          a.cfg.UDPListen,
			"stun_alt_listen":     a.cfg.StunAltListen,
			"http_listen":         a.cfg.HTTPListen,
			"database_path":       a.cfg.DatabasePath,
			"psk_configured":      a.cfg.PSK != "",
			"peer_ttl":            a.cfg.PeerTTL.String(),
			"pair_ttl":            a.cfg.PairTTL.String(),
			"relay_idle_timeout":  a.cfg.RelayIdleTimeout.String(),
			"allow_relay":         a.cfg.AllowRelay,
			"allow_legacy":        a.cfg.AllowLegacy,
			"restart_only_fields": []string{"udp_listen", "stun_alt_listen", "http_listen", "database_path", "psk"},
		}
		a.cfgMu.RUnlock()
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPatch:
		var req struct {
			PeerTTL          string `json:"peer_ttl"`
			PairTTL          string `json:"pair_ttl"`
			RelayIdleTimeout string `json:"relay_idle_timeout"`
			AllowRelay       bool   `json:"allow_relay"`
			AllowLegacy      bool   `json:"allow_legacy"`
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
		a.cfgMu.Lock()
		a.cfg.PeerTTL = peerTTL
		a.cfg.PairTTL = pairTTL
		a.cfg.RelayIdleTimeout = relayIdle
		a.cfg.AllowRelay = req.AllowRelay
		a.cfg.AllowLegacy = req.AllowLegacy
		a.cfgMu.Unlock()
		_ = a.db.PutMeta(r.Context(), "setting_peer_ttl", peerTTL.String())
		_ = a.db.PutMeta(r.Context(), "setting_pair_ttl", pairTTL.String())
		_ = a.db.PutMeta(r.Context(), "setting_relay_idle_timeout", relayIdle.String())
		_ = a.db.PutMeta(r.Context(), "setting_allow_relay", strconv.FormatBool(req.AllowRelay))
		_ = a.db.PutMeta(r.Context(), "setting_allow_legacy", strconv.FormatBool(req.AllowLegacy))
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
		DeviceID string `json:"device_id"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	err := a.db.UpsertDevice(r.Context(), req.DeviceID, "", "", "", true)
	writeJSONOrError(w, map[string]any{"ok": true}, err)
}

func (a *app) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	err := a.db.UpsertDevice(r.Context(), req.DeviceID, "", "", "", true)
	writeJSONOrError(w, map[string]any{"ok": true}, err)
}

func (a *app) handleAgentRules(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if deviceID == "" {
		http.Error(w, "device_id required", http.StatusBadRequest)
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

func writeJSONOrError(w http.ResponseWriter, v any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sqlErrNoRows()) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
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
