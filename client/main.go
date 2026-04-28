package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/xtaci/smux"

	"udp_tunnel_demo/internal/config"
	"udp_tunnel_demo/internal/forward"
	"udp_tunnel_demo/internal/protocol"
	"udp_tunnel_demo/internal/secure"
	"udp_tunnel_demo/internal/store"
	"udp_tunnel_demo/internal/tunnel"
	"udp_tunnel_demo/internal/upnp"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

type agentTunnelStatus struct {
	Peer       string `json:"peer"`
	State      string `json:"state"`
	Via        string `json:"via"`
	PublicAddr string `json:"public_addr"`
	ConvID     int64  `json:"conv_id"`
	RTTMs      int    `json:"rtt_ms"`
	NATType    string `json:"nat_type"`
	LastError  string `json:"last_error"`
}

type agentPeerSession struct {
	peer       string
	sig        string
	ruleIDs    []int64
	localPorts []int

	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.RWMutex
	status agentTunnelStatus
	addr   string
	upnp   string
}

type peerRuleSet struct {
	Peer       string
	Forward    []forward.Rule
	RuleIDs    []int64
	LocalPorts []int
}

type natProbeResult struct {
	NATType     string
	PrimaryAddr string
	AltAddr     string
	ForceRelay  bool
}

type bootstrapResponse struct {
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

func (s *agentPeerSession) setStatus(st agentTunnelStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = st
}

func (s *agentPeerSession) setDeviceAddrs(addr, upnp string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if addr != "" {
		s.addr = addr
	}
	if upnp != "" {
		s.upnp = upnp
	}
}

func (s *agentPeerSession) snapshot() (agentTunnelStatus, string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status, s.addr, s.upnp
}

func main() {
	cfg := config.DefaultClient()
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	configPath := fs.String("config", "client.json", "client config file")
	serverAddr := fs.String("server", "", "Rendezvous server address, e.g. 1.2.3.4:7000")
	serverHTTP := fs.String("server-http", "", "Control plane HTTP URL, e.g. http://1.2.3.4:7001")
	id := fs.String("id", "", "my device ID")
	peerID := fs.String("peer", "", "peer device ID to connect")
	psk := fs.String("psk", "", "deployment pre-shared key")
	probe := fs.Bool("probe", false, "NAT probe mode")
	altPort := fs.Int("alt-port", 7002, "server STUN alternate port")
	punchTimeout := fs.Duration("punch-timeout", cfg.PunchTimeout, "hole punching timeout")
	forceRelay := fs.Bool("force-relay", false, "skip punching and use relay")
	noUpnp := fs.Bool("no-upnp", cfg.NoUPnP, "disable UPnP mapping")
	upnpTimeout := fs.Duration("upnp-timeout", cfg.UPnPTimeout, "UPnP timeout")
	agent := fs.Bool("agent", false, "run as product agent and pull rules from control plane")
	var forwards multiFlag
	fs.Var(&forwards, "forward", "TCP forward rule LOCAL:HOST:PORT")
	fs.Parse(os.Args[1:])

	_ = config.LoadJSON(*configPath, &cfg)
	if flagSet(fs, "server") {
		cfg.Server = *serverAddr
	}
	if flagSet(fs, "server-http") {
		cfg.ServerHTTP = *serverHTTP
	}
	if flagSet(fs, "id") {
		cfg.DeviceID = *id
	}
	if flagSet(fs, "peer") {
		cfg.PeerID = *peerID
	}
	if flagSet(fs, "psk") {
		cfg.PSK = *psk
	}
	if flagSet(fs, "no-upnp") {
		cfg.NoUPnP = *noUpnp
	}
	if flagSet(fs, "upnp-timeout") {
		cfg.UPnPTimeout = *upnpTimeout
	}
	if flagSet(fs, "punch-timeout") {
		cfg.PunchTimeout = *punchTimeout
	}
	if flagSet(fs, "force-relay") {
		cfg.ForceRelay = *forceRelay
	}
	if len(forwards) > 0 {
		cfg.Forwards = forwards
	}
	if err := ensureLocalIdentity(&cfg, *configPath); err != nil {
		log.Fatal(err)
	}
	logPath := setupLogging(cfg.DeviceID)
	logStartup(cfg, *configPath, logPath, *agent, *probe)
	if needsBootstrapConfig(cfg, *probe, len(forwards) > 0 || flagSet(fs, "peer")) {
		runBootstrapConfigMode(&cfg, *configPath)
		return
	}

	runtimeCfg := cfg
	bootstrapAltPort := *altPort
	if cfg.ServerHTTP != "" {
		if resp, err := agentBootstrap(cfg); err != nil {
			if cfg.Server == "" {
				log.Printf("[%s] bootstrap failed: %v", cfg.DeviceID, err)
				runBootstrapConfigMode(&cfg, *configPath)
				return
			}
		} else {
			if merged, err := mergeBootstrap(cfg, resp); err != nil {
				log.Printf("[%s] bad bootstrap config: %v", cfg.DeviceID, err)
				runBootstrapConfigMode(&cfg, *configPath)
				return
			} else {
				runtimeCfg = merged
				bootstrapAltPort = resp.STUNAltPort
			}
		}
	}
	if runtimeCfg.DeviceID != cfg.DeviceID {
		log.Printf("[%s] bootstrap updated device_id -> %s", cfg.DeviceID, runtimeCfg.DeviceID)
	}

	if runtimeCfg.Server == "" {
		log.Printf("[%s] runtime server is empty after bootstrap", runtimeCfg.DeviceID)
		runBootstrapConfigMode(&cfg, *configPath)
		return
	}
	if *probe {
		runProbe(runtimeCfg.Server, bootstrapAltPort, runtimeCfg.PSK, runtimeCfg.AllowLegacy)
		return
	}
	natResult := autoDetectNAT(runtimeCfg, bootstrapAltPort)
	if *agent || (runtimeCfg.ServerHTTP != "" && runtimeCfg.DeviceID != "" && runtimeCfg.PeerID == "") {
		runAgent(cfg, runtimeCfg, *configPath, natResult)
		return
	}
	if runtimeCfg.DeviceID == "" || runtimeCfg.PeerID == "" {
		log.Fatal("usage: -server <ip:port> or -server-http <url> -id <me> -peer <other> [-forward LOCAL:HOST:PORT ...]")
	}
	for {
		err := runTunnelOnce(context.Background(), runtimeCfg, runtimeCfg.PeerID, parseForwardRules(runtimeCfg.Forwards, runtimeCfg.DeviceID, runtimeCfg.PeerID), natResult, func(agentTunnelStatus, string, string) {})
		log.Printf("[%s] tunnel stopped: %v", runtimeCfg.DeviceID, err)
		time.Sleep(2 * time.Second)
	}
}

func needsBootstrapConfig(cfg config.Client, probeMode bool, explicitPeer bool) bool {
	if probeMode {
		return false
	}
	if cfg.Server != "" {
		return false
	}
	if explicitPeer && cfg.ServerHTTP == "" {
		return false
	}
	return strings.TrimSpace(cfg.ServerHTTP) == "" || strings.TrimSpace(cfg.PSK) == ""
}

func runBootstrapConfigMode(cfg *config.Client, configPath string) {
	configURL := startClientConfigServer(cfg, configPath, restartSelf)
	if configURL != "" {
		log.Printf("[%s] bootstrap config required; opening local settings page: %s", cfg.DeviceID, configURL)
		openBrowser(configURL)
	} else {
		log.Printf("[%s] bootstrap config required; local settings page could not start", cfg.DeviceID)
	}
	log.Printf("[%s] missing local bootstrap config: server_http=%q psk_set=%v", cfg.DeviceID, cfg.ServerHTTP, cfg.PSK != "")
	waitForSignal(cfg.DeviceID)
}

func waitForSignal(deviceID string) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Printf("[%s] signal received, shutting down", deviceID)
}

func runAgent(localCfg, runtimeCfg config.Client, configPath string, natResult natProbeResult) {
	if runtimeCfg.ServerHTTP == "" {
		runtimeCfg.ServerHTTP = udpToHTTP(runtimeCfg.Server)
	}
	configURL := startClientConfigServer(&localCfg, configPath, restartSelf)
	log.Printf("[%s] agent starting (control=%s tray=%v name=%q)", runtimeCfg.DeviceID, runtimeCfg.ServerHTTP, runtimeCfg.TrayEnabled, runtimeCfg.DeviceName)
	if runtimeCfg.TrayEnabled {
		go runAgentLoop(runtimeCfg, natResult)
		runTray(runtimeCfg.DeviceID, runtimeCfg.ServerHTTP, configURL, func() { os.Exit(0) })
		return
	}
	runAgentLoop(runtimeCfg, natResult)
}

func restartSelf() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("restart failed: os.Executable: %v", err)
		os.Exit(0)
		return
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	if err := cmd.Start(); err != nil {
		log.Printf("restart failed: %v", err)
		os.Exit(0)
		return
	}
	log.Printf("restarted client pid=%d", cmd.Process.Pid)
	os.Exit(0)
}

func runAgentLoop(runtimeCfg config.Client, natResult natProbeResult) {
	if err := agentPost(runtimeCfg, "/api/agent/register", map[string]any{
		"device_id": runtimeCfg.DeviceID,
		"name":      runtimeCfg.DeviceName,
		"nat_type":  natResult.NATType,
	}); err != nil {
		log.Printf("[%s] agent register failed: %v", runtimeCfg.DeviceID, err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	rootCtx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()
	sessions := map[string]*agentPeerSession{}
	var lastEmptyLog time.Time
	var lastRulesSig string
	for {
		select {
		case <-stop:
			log.Printf("[%s] signal received, shutting down", runtimeCfg.DeviceID)
			cancelAll()
			for _, sess := range sessions {
				<-sess.done
			}
			return
		default:
		}
		rules, err := agentRules(runtimeCfg)
		if err != nil {
			log.Printf("[%s] pull rules failed: %v", runtimeCfg.DeviceID, err)
			time.Sleep(5 * time.Second)
			continue
		}
		sig := rulesSignature(rules)
		if sig != lastRulesSig {
			log.Printf("[%s] rules updated: %d total -> %s", runtimeCfg.DeviceID, len(rules), sig)
			lastRulesSig = sig
		}
		grouped := groupRulesByPeer(runtimeCfg.DeviceID, rules)
		if len(grouped) == 0 {
			if time.Since(lastEmptyLog) > 60*time.Second {
				log.Printf("[%s] no enabled rule references this device (rules=%d); waiting", runtimeCfg.DeviceID, len(rules))
				lastEmptyLog = time.Now()
			}
		}
		for peer, sess := range sessions {
			nextSet, ok := grouped[peer]
			nextSig := forwardRulesSignature(nextSet)
			if ok && sess.sig == nextSig {
				continue
			}
			log.Printf("[%s] peer=%s rules=%v local_ports=%v stopping: rule removed or changed", runtimeCfg.DeviceID, peer, sess.ruleIDs, sess.localPorts)
			sess.setStatus(agentTunnelStatus{Peer: peer, State: "down", LastError: "rule_changed"})
			_ = agentPost(runtimeCfg, "/api/agent/tunnel-status", map[string]any{
				"device_id": runtimeCfg.DeviceID,
				"peer":      peer,
				"state":     "down",
				"last_error": "rule_changed",
			})
			sess.cancel()
			<-sess.done
			delete(sessions, peer)
		}
		for peer, ruleSet := range grouped {
			if _, ok := sessions[peer]; ok {
				continue
			}
			sctx, scancel := context.WithCancel(rootCtx)
			sess := &agentPeerSession{
				peer:       peer,
				sig:        forwardRulesSignature(ruleSet),
				ruleIDs:    append([]int64(nil), ruleSet.RuleIDs...),
				localPorts: append([]int(nil), ruleSet.LocalPorts...),
				cancel:     scancel,
				done:       make(chan struct{}),
			}
			sess.setStatus(agentTunnelStatus{Peer: peer, State: "connecting"})
			sessions[peer] = sess
			log.Printf("[%s] peer=%s rule_ids=%v local_ports=%v activating ingress_rules=%d", runtimeCfg.DeviceID, peer, ruleSet.RuleIDs, ruleSet.LocalPorts, len(ruleSet.Forward))
			go func(peer string, rules peerRuleSet, st *agentPeerSession) {
				defer close(st.done)
				report := func(ts agentTunnelStatus, addr, upnp string) {
					ts.Peer = peer
					st.setStatus(ts)
					st.setDeviceAddrs(addr, upnp)
					if ts.State == "" {
						return
					}
					body := map[string]any{
						"device_id":   runtimeCfg.DeviceID,
						"name":        runtimeCfg.DeviceName,
						"peer":        peer,
						"state":       ts.State,
						"via":         ts.Via,
						"public_addr": ts.PublicAddr,
						"conv_id":     ts.ConvID,
						"rtt_ms":      ts.RTTMs,
						"nat_type":    ts.NATType,
						"last_error":  ts.LastError,
						"addr":        addr,
						"upnp_addr":   upnp,
					}
					if err := agentPost(runtimeCfg, "/api/agent/tunnel-status", body); err != nil && sctx.Err() == nil {
						log.Printf("[%s] tunnel-status peer=%s failed: %v", runtimeCfg.DeviceID, peer, err)
					}
				}
				report(agentTunnelStatus{Peer: peer, State: "connecting"}, "", "")
				for {
					err := runTunnelOnce(sctx, runtimeCfg, peer, rules.Forward, natResult, report)
					if sctx.Err() != nil {
						report(agentTunnelStatus{Peer: peer, State: "down", LastError: "rule_cancelled"}, "", "")
						return
					}
					reason := classifyTunnelError(err)
					report(agentTunnelStatus{Peer: peer, State: "down", LastError: reason}, "", "")
					log.Printf("[%s] peer=%s rule_ids=%v local_ports=%v tunnel stopped: %s (%v)", runtimeCfg.DeviceID, peer, rules.RuleIDs, rules.LocalPorts, reason, err)
					select {
					case <-time.After(3 * time.Second):
						report(agentTunnelStatus{Peer: peer, State: "connecting"}, "", "")
					case <-sctx.Done():
						report(agentTunnelStatus{Peer: peer, State: "down", LastError: "rule_cancelled"}, "", "")
						return
					}
				}
			}(peer, ruleSet, sess)
		}
		addr, upnp, tunnels := snapshotAgentSessions(sessions)
		if err := agentPost(runtimeCfg, "/api/agent/heartbeat", map[string]any{
			"device_id": runtimeCfg.DeviceID,
			"name":      runtimeCfg.DeviceName,
			"addr":      addr,
			"upnp_addr": upnp,
			"nat_type":  natResult.NATType,
			"tunnels":   tunnels,
		}); err != nil {
			log.Printf("[%s] heartbeat failed: %v", runtimeCfg.DeviceID, err)
		}
		time.Sleep(10 * time.Second)
	}
}

func runTunnelOnce(ctx context.Context, cfg config.Client, peerID string, rules []forward.Rule, natResult natProbeResult, report func(agentTunnelStatus, string, string)) error {
	var codec *secure.Codec
	var err error
	if cfg.PSK != "" {
		codec, err = secure.NewCodec(cfg.PSK)
		if err != nil {
			return err
		}
	}
	srvAddr, err := net.ResolveUDPAddr("udp", cfg.Server)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return fmt.Errorf("local_socket_failed: %w", err)
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	log.Printf("[%s] peer=%s local socket=%s", cfg.DeviceID, peerID, conn.LocalAddr())

	var upnpMapping *upnp.Mapping
	var upnpAddrStr string
	if !cfg.NoUPnP {
		localPort := conn.LocalAddr().(*net.UDPAddr).Port
		ctx, cancel := context.WithTimeout(context.Background(), cfg.UPnPTimeout+time.Second)
		m, uerr := upnp.Try(ctx, localPort, fmt.Sprintf("udp-tunnel %s", cfg.DeviceID), cfg.UPnPTimeout)
		cancel()
		if uerr != nil {
			log.Printf("[%s] peer=%s upnp failed: %v", cfg.DeviceID, peerID, uerr)
		} else {
			upnpMapping = m
			upnpAddrStr = m.External()
			log.Printf("[%s] peer=%s upnp mapped: %s -> :%d", cfg.DeviceID, peerID, upnpAddrStr, m.InternalPort)
			defer upnpMapping.Close()
			go refreshUPnPMapping(ctx, cfg.DeviceID, upnpMapping)
		}
	}
	report(agentTunnelStatus{Peer: peerID, State: "connecting", NATType: natResult.NATType}, "", upnpAddrStr)

	writeControl := func(dst *net.UDPAddr, msg *protocol.Message) error {
		b, _ := protocol.Encode(msg)
		if codec != nil {
			b, err = codec.Seal(secure.KindControl, b)
			if err != nil {
				return err
			}
		}
		_, err = conn.WriteToUDP(b, dst)
		return err
	}
	register := func() error {
		return writeControl(srvAddr, &protocol.Message{Type: protocol.MsgRegister, From: cfg.DeviceID, Name: cfg.DeviceName, Peer: peerID, UpnpAddr: upnpAddrStr})
	}
	if err := register(); err != nil {
		return fmt.Errorf("register_failed: %w", err)
	}
	log.Printf("[%s] peer=%s registered to server, waiting for peer info...", cfg.DeviceID, peerID)

	var peerAddr atomic.Pointer[net.UDPAddr]
	var peerUpnpAddr atomic.Pointer[net.UDPAddr]
	var punched atomic.Bool
	var punchStarted atomic.Bool
	var isRelay atomic.Bool
	var rxCount atomic.Uint64
	punchedCh := make(chan struct{})
	var closeOnce struct{ done atomic.Bool }

	enterRelay := func(reason string) {
		if isRelay.CompareAndSwap(false, true) {
			log.Printf("[%s] peer=%s relay mode (%s), peerAddr -> %s", cfg.DeviceID, peerID, reason, srvAddr)
			peerAddr.Store(srvAddr)
			punched.Store(true)
			if closeOnce.done.CompareAndSwap(false, true) {
				close(punchedCh)
			}
		}
	}

	var pc *tunnel.PacketConn
	if codec != nil {
		pc = tunnel.NewSecurePacketConn(conn, &peerAddr, codec)
	} else {
		pc = tunnel.NewPacketConn(conn, &peerAddr)
	}

	go func() {
		buf := make([]byte, 65535)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			data := append([]byte(nil), buf[:n]...)
			if secure.IsFrame(data) {
				if codec == nil {
					continue
				}
				kind, payload, err := codec.Open(data)
				if err != nil {
					continue
				}
				if kind == secure.KindKCP {
					pc.Feed(payload, src)
					continue
				}
				data = payload
			} else if !cfg.AllowLegacy && len(data) > 0 && data[0] != '{' {
				pc.Feed(data, src)
				continue
			} else if len(data) > 0 && data[0] != '{' {
				pc.Feed(data, src)
				continue
			}
			msg, err := protocol.Decode(data)
			if err != nil {
				continue
			}
			switch msg.Type {
			case protocol.MsgPeerInfo:
				if isRelay.Load() {
					continue
				}
				paddr, err := net.ResolveUDPAddr("udp", msg.Addr)
				if err != nil {
					log.Printf("[%s] peer=%s bad peer addr: %v", cfg.DeviceID, peerID, err)
					continue
				}
				peerAddr.Store(paddr)
				if msg.UpnpAddr != "" {
					if uaddr, uerr := net.ResolveUDPAddr("udp", msg.UpnpAddr); uerr == nil {
						peerUpnpAddr.Store(uaddr)
					}
				}
				if punchStarted.CompareAndSwap(false, true) {
					go punchLoop(conn, &peerAddr, &peerUpnpAddr, &punched, cfg.DeviceID, writeControl)
				}
			case protocol.MsgPunch:
				rxCount.Add(1)
				adoptPeerAddrSafe(&peerAddr, src, cfg.DeviceID, &isRelay)
				_ = writeControl(src, &protocol.Message{Type: protocol.MsgPunchAck, From: cfg.DeviceID})
				if punched.CompareAndSwap(false, true) {
					log.Printf("[%s] peer=%s hole punched via incoming punch: %s", cfg.DeviceID, peerID, src)
					if closeOnce.done.CompareAndSwap(false, true) {
						close(punchedCh)
					}
				}
			case protocol.MsgPunchAck:
				rxCount.Add(1)
				adoptPeerAddrSafe(&peerAddr, src, cfg.DeviceID, &isRelay)
				if punched.CompareAndSwap(false, true) {
					log.Printf("[%s] peer=%s hole punched via ack: %s", cfg.DeviceID, peerID, src)
					if closeOnce.done.CompareAndSwap(false, true) {
						close(punchedCh)
					}
				}
			}
		}
	}()

	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if paddr := peerAddr.Load(); punched.Load() && paddr != nil {
				_ = writeControl(paddr, &protocol.Message{Type: protocol.MsgKeepAlive, From: cfg.DeviceID})
			}
		}
	}()

	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if punched.Load() {
				return
			}
			if err := register(); err != nil {
				log.Printf("[%s] peer=%s register refresh failed: %v", cfg.DeviceID, peerID, err)
			}
		}
	}()

	if cfg.ForceRelay || natResult.ForceRelay {
		reason := "-force-relay"
		if !cfg.ForceRelay && natResult.ForceRelay {
			reason = "auto-relay symmetric nat"
		}
		enterRelay(reason)
	}
	go func() {
		t := time.NewTimer(cfg.PunchTimeout)
		defer t.Stop()
		select {
		case <-punchedCh:
		case <-ctx.Done():
		case <-t.C:
			enterRelay(fmt.Sprintf("punch timeout %s", cfg.PunchTimeout))
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-punchedCh:
	}

	isListener := cfg.DeviceID < peerID
	role := "dialer"
	if isListener {
		role = "listener"
	}
	convID := secure.ConvID(cfg.PSK, cfg.DeviceID, peerID)
	log.Printf("[%s] peer=%s opening KCP tunnel as %s conv=%d", cfg.DeviceID, peerID, role, convID)
	time.Sleep(300 * time.Millisecond)

	kcpConn, err := tunnel.Open(pc, isListener, convID)
	if err != nil {
		return err
	}
	defer kcpConn.Close()
	go func() {
		<-ctx.Done()
		_ = kcpConn.Close()
	}()
	if isListener {
		if err := tunnel.ConsumeHandshake(kcpConn); err != nil {
			return err
		}
	}
	smuxCfg := smux.DefaultConfig()
	smuxCfg.Version = 2
	smuxCfg.KeepAliveInterval = 10 * time.Second
	smuxCfg.KeepAliveTimeout = 30 * time.Second
	var mux *smux.Session
	if isListener {
		mux, err = smux.Server(kcpConn, smuxCfg)
	} else {
		mux, err = smux.Client(kcpConn, smuxCfg)
	}
	if err != nil {
		return err
	}
	defer mux.Close()
	go func() {
		<-ctx.Done()
		_ = mux.Close()
	}()
	via := "p2p"
	if isRelay.Load() {
		via = "relay"
	}
	report(agentTunnelStatus{
		Peer:       peerID,
		State:      via,
		Via:        via,
		PublicAddr: upnpAddrStr,
		ConvID:     int64(convID),
		RTTMs:      tunnelSessionRTT(kcpConn),
		NATType:    natResult.NATType,
	}, "", upnpAddrStr)
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-mux.CloseChan():
				return
			case <-t.C:
				report(agentTunnelStatus{
					Peer:       peerID,
					State:      via,
					Via:        via,
					PublicAddr: upnpAddrStr,
					ConvID:     int64(convID),
					RTTMs:      tunnelSessionRTT(kcpConn),
					NATType:    natResult.NATType,
				}, "", upnpAddrStr)
			}
		}
	}()
	go forward.RunEgress(mux)
	if len(rules) > 0 {
		if err := forward.RunIngress(mux, rules); err != nil {
			return fmt.Errorf("ingress_listen_failed: %w", err)
		}
	} else {
		log.Printf("[%s] peer=%s no local forward rules; egress only", cfg.DeviceID, peerID)
	}
	<-mux.CloseChan()
	return fmt.Errorf("kcp_eof: %w", io.EOF)
}

func agentPost(cfg config.Client, path string, body any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.ServerHTTP, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-UDP-Tunnel-PSK", cfg.PSK)
	res, err := doAgentHTTPRequest(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("http %d", res.StatusCode)
	}
	return nil
}

func agentRules(cfg config.Client) ([]store.ForwardRule, error) {
	u := strings.TrimRight(cfg.ServerHTTP, "/") + "/api/agent/rules?device_id=" + url.QueryEscape(cfg.DeviceID)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("X-UDP-Tunnel-PSK", cfg.PSK)
	res, err := doAgentHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d", res.StatusCode)
	}
	var rules []store.ForwardRule
	return rules, json.NewDecoder(res.Body).Decode(&rules)
}

// groupRulesByPeer 把控制面下发的所有规则按对端 ID 分组：
//   - 我是 source：把规则当 ingress 加到 grouped[target]
//   - 我是 target：确保 grouped[source] 存在（egress 端不需要本地 forward.Rule）
//
// 返回的每个 key 都是一个独立的 peer，agent 会为它单独起 socket、register、隧道。
func groupRulesByPeer(deviceID string, rules []store.ForwardRule) map[string]peerRuleSet {
	grouped := map[string]peerRuleSet{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		switch deviceID {
		case r.SourceID:
			set := grouped[r.TargetID]
			set.Peer = r.TargetID
			set.Forward = append(set.Forward, forward.Rule{
				LocalPort: r.LocalPort,
				Target:    fmt.Sprintf("%s:%d", r.TargetHost, r.TargetPort),
				Name:      r.Name,
			})
			set.RuleIDs = append(set.RuleIDs, r.ID)
			set.LocalPorts = append(set.LocalPorts, r.LocalPort)
			grouped[r.TargetID] = set
		case r.TargetID:
			if _, ok := grouped[r.SourceID]; !ok {
				grouped[r.SourceID] = peerRuleSet{Peer: r.SourceID}
			}
		}
	}
	return grouped
}

func forwardRulesSignature(rules peerRuleSet) string {
	parts := make([]string, 0, len(rules.Forward))
	for i, r := range rules.Forward {
		ruleID := int64(0)
		if i < len(rules.RuleIDs) {
			ruleID = rules.RuleIDs[i]
		}
		parts = append(parts, fmt.Sprintf("#%d:%s:%d->%s", ruleID, r.Name, r.LocalPort, r.Target))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func snapshotAgentSessions(sessions map[string]*agentPeerSession) (string, string, []agentTunnelStatus) {
	peers := make([]string, 0, len(sessions))
	for peer := range sessions {
		peers = append(peers, peer)
	}
	sort.Strings(peers)
	out := make([]agentTunnelStatus, 0, len(peers))
	var addr, upnp string
	for _, peer := range peers {
		st, a, u := sessions[peer].snapshot()
		if st.Peer == "" {
			st.Peer = peer
		}
		out = append(out, st)
		if addr == "" && a != "" {
			addr = a
		}
		if upnp == "" && u != "" {
			upnp = u
		}
	}
	return addr, upnp, out
}

func classifyTunnelError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case errors.Is(err, context.Canceled):
		return "rule_cancelled"
	case strings.Contains(msg, "register_failed"):
		return "register_failed"
	case strings.Contains(msg, "ingress_listen_failed"):
		return "ingress_listen_failed"
	case strings.Contains(msg, "local_socket_failed"):
		return "local_socket_failed"
	case strings.Contains(msg, "kcp_eof"):
		return "kcp_eof"
	case strings.Contains(msg, "i/o timeout"):
		return "io_timeout"
	default:
		return msg
	}
}

func ensureLocalIdentity(cfg *config.Client, configPath string) error {
	changed := false
	if strings.TrimSpace(cfg.DeviceName) == "" {
		cfg.DeviceName = defaultDeviceName()
		changed = true
	}
	if strings.TrimSpace(cfg.DeviceID) == "" && cfg.PeerID == "" {
		cfg.DeviceID = generateDeviceID()
		changed = true
	}
	if changed && configPath != "" && cfg.ServerHTTP != "" {
		if err := config.SaveClientLocalJSON(configPath, *cfg); err != nil {
			return err
		}
	}
	return nil
}

func generateDeviceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "dev-" + strings.ToLower(strings.ReplaceAll(time.Now().UTC().Format("20060102150405"), " ", ""))
	}
	return "dev-" + strings.ToLower(hex.EncodeToString(b[:]))
}

func agentBootstrap(cfg config.Client) (bootstrapResponse, error) {
	body := map[string]any{
		"device_id":   cfg.DeviceID,
		"device_name": cfg.DeviceName,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.ServerHTTP, "/")+"/api/agent/bootstrap", bytes.NewReader(b))
	if err != nil {
		return bootstrapResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-UDP-Tunnel-PSK", cfg.PSK)
	res, err := doAgentHTTPRequest(req)
	if err != nil {
		return bootstrapResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return bootstrapResponse{}, fmt.Errorf("http %d", res.StatusCode)
	}
	var resp bootstrapResponse
	return resp, json.NewDecoder(res.Body).Decode(&resp)
}

func doAgentHTTPRequest(req *http.Request) (*http.Response, error) {
	res, err := http.DefaultClient.Do(req)
	if err == nil || !shouldRetryDirect(err) {
		return res, err
	}
	log.Printf("agent http via proxy failed, retry direct: %v", err)
	directReq := req.Clone(req.Context())
	if req.GetBody != nil && req.Body != nil {
		body, gerr := req.GetBody()
		if gerr != nil {
			return nil, err
		}
		directReq.Body = body
	}
	directTransport := http.DefaultTransport.(*http.Transport).Clone()
	directTransport.Proxy = nil
	return (&http.Client{Transport: directTransport, Timeout: 15 * time.Second}).Do(directReq)
}

func shouldRetryDirect(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "proxyconnect tcp") ||
		(strings.Contains(msg, "proxy") && strings.Contains(msg, "connectex")) ||
		(strings.Contains(msg, "proxy") && strings.Contains(msg, "connection refused"))
}

func mergeBootstrap(local config.Client, resp bootstrapResponse) (config.Client, error) {
	cfg := local
	cfg.Server = resp.Server
	cfg.ServerHTTP = resp.ServerHTTP
	if resp.DeviceID != "" {
		cfg.DeviceID = resp.DeviceID
	}
	if resp.DeviceName != "" {
		cfg.DeviceName = resp.DeviceName
	}
	cfg.NoUPnP = resp.NoUPnP
	upnpTimeout, err := time.ParseDuration(resp.UPnPTimeout)
	if err != nil {
		return cfg, err
	}
	cfg.UPnPTimeout = upnpTimeout
	cfg.LogLevel = resp.LogLevel
	cfg.TrayEnabled = resp.TrayEnabled
	punchTimeout, err := time.ParseDuration(resp.PunchTimeout)
	if err != nil {
		return cfg, err
	}
	cfg.PunchTimeout = punchTimeout
	cfg.ForceRelay = resp.ForceRelay
	cfg.AllowLegacy = resp.AllowLegacy
	return cfg, nil
}

func autoDetectNAT(cfg config.Client, altPort int) natProbeResult {
	if cfg.ForceRelay {
		log.Printf("[%s] NAT auto-detect skipped: force-relay already enabled", cfg.DeviceID)
		return natProbeResult{NATType: "manual-relay", ForceRelay: true}
	}
	res, err := probeNAT(cfg.Server, altPort, cfg.PSK, cfg.AllowLegacy)
	if err != nil {
		log.Printf("[%s] NAT auto-detect failed: %v; keep default punch-then-relay", cfg.DeviceID, err)
		return natProbeResult{NATType: "unknown"}
	}
	switch res.NATType {
	case "symmetric":
		log.Printf("[%s] NAT auto-detect: symmetric (%s / %s), enable relay-first", cfg.DeviceID, res.PrimaryAddr, res.AltAddr)
		res.ForceRelay = true
	case "cone":
		log.Printf("[%s] NAT auto-detect: cone-like (%s / %s), keep p2p first", cfg.DeviceID, res.PrimaryAddr, res.AltAddr)
	default:
		log.Printf("[%s] NAT auto-detect: %s", cfg.DeviceID, res.NATType)
	}
	return res
}

func parseForwardRules(values []string, id, peerID string) []forward.Rule {
	var rules []forward.Rule
	for _, s := range values {
		r, err := forward.ParseRule(s)
		if err != nil {
			log.Fatalf("bad -forward %q: %v", s, err)
		}
		rules = append(rules, r)
	}
	return rules
}

func runProbe(server string, altPort int, psk string, allowLegacy bool) {
	res, err := probeNAT(server, altPort, psk, allowLegacy)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("========== NAT 探测结果 ==========")
	fmt.Printf("主端口观察到: %s\n备端口观察到: %s\n", res.PrimaryAddr, res.AltAddr)
	switch res.NATType {
	case "cone":
		fmt.Println("类型判定: Cone 类 NAT")
	case "symmetric":
		fmt.Println("类型判定: Symmetric NAT")
	default:
		fmt.Printf("类型判定: %s\n", res.NATType)
	}
}

func probeNAT(server string, altPort int, psk string, allowLegacy bool) (natProbeResult, error) {
	host, _, err := net.SplitHostPort(server)
	if err != nil {
		return natProbeResult{}, fmt.Errorf("bad server: %w", err)
	}
	primary, _ := net.ResolveUDPAddr("udp", server)
	alt, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, altPort))
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return natProbeResult{}, err
	}
	defer conn.Close()
	var codec *secure.Codec
	if psk != "" {
		codec, _ = secure.NewCodec(psk)
	}
	ask := func(target *net.UDPAddr, label string) (string, error) {
		req := &protocol.Message{Type: protocol.MsgStunReq}
		b, _ := protocol.Encode(req)
		if codec != nil {
			b, _ = codec.Seal(secure.KindControl, b)
		}
		_, _ = conn.WriteToUDP(b, target)
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 4096)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return "", fmt.Errorf("no reply from %s (%s): %w", target, label, err)
			}
			if src.String() != target.String() {
				continue
			}
			data := buf[:n]
			if secure.IsFrame(data) && codec != nil {
				kind, payload, err := codec.Open(data)
				if err != nil || kind != secure.KindControl {
					continue
				}
				data = payload
			} else if !allowLegacy {
				continue
			}
			msg, err := protocol.Decode(data)
			if err == nil && msg.Type == protocol.MsgStunResp {
				log.Printf("%s: server %s sees me as %s", label, target, msg.Addr)
				return msg.Addr, nil
			}
		}
	}
	a1, err := ask(primary, "probe#1")
	if err != nil {
		return natProbeResult{}, err
	}
	a2, err := ask(alt, "probe#2")
	if err != nil {
		return natProbeResult{}, err
	}
	_, p1, _ := net.SplitHostPort(a1)
	_, p2, _ := net.SplitHostPort(a2)
	res := natProbeResult{
		NATType:     "unknown",
		PrimaryAddr: a1,
		AltAddr:     a2,
	}
	if p1 == p2 {
		res.NATType = "cone"
	} else {
		res.NATType = "symmetric"
		res.ForceRelay = true
	}
	return res, nil
}

func adoptPeerAddr(peerAddr *atomic.Pointer[net.UDPAddr], src *net.UDPAddr, id string) {
	cur := peerAddr.Load()
	if cur == nil || cur.String() != src.String() {
		cp := *src
		peerAddr.Store(&cp)
		if cur != nil {
			log.Printf("[%s] peer addr learned: %s -> %s", id, cur, src)
		}
	}
}

func adoptPeerAddrSafe(peerAddr *atomic.Pointer[net.UDPAddr], src *net.UDPAddr, id string, isRelay *atomic.Bool) {
	if !isRelay.Load() {
		adoptPeerAddr(peerAddr, src, id)
	}
}

func punchLoop(conn *net.UDPConn, peerAddr, peerUpnpAddr *atomic.Pointer[net.UDPAddr], punched *atomic.Bool, id string, writeControl func(*net.UDPAddr, *protocol.Message) error) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	attempts := 0
	for range t.C {
		if punched.Load() {
			return
		}
		paddr := peerAddr.Load()
		if paddr == nil {
			return
		}
		msg := &protocol.Message{Type: protocol.MsgPunch, From: id}
		_ = writeControl(paddr, msg)
		if uaddr := peerUpnpAddr.Load(); uaddr != nil && uaddr.String() != paddr.String() {
			_ = writeControl(uaddr, msg)
		}
		attempts++
		if attempts%4 == 0 {
			log.Printf("[%s] punching... sent=%d target=%s", id, attempts, paddr)
		}
	}
}

func refreshUPnPMapping(ctx context.Context, deviceID string, mapping *upnp.Mapping) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := mapping.Refresh(rctx)
		cancel()
		if err != nil {
			log.Printf("[%s] UPnP refresh failed: %v", deviceID, err)
			continue
		}
		log.Printf("[%s] UPnP mapping refreshed: %s", deviceID, mapping.External())
	}
}

func tunnelSessionRTT(conn net.Conn) int {
	_, rtt, ok := tunnel.SessionStats(conn)
	if !ok || rtt < 0 {
		return 0
	}
	return rtt
}

func udpToHTTP(server string) string {
	host, _, err := net.SplitHostPort(server)
	if err != nil {
		return "http://" + server
	}
	return "http://" + net.JoinHostPort(host, "7001")
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

func defaultDeviceName() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "windows-client"
	}
	return strings.TrimSpace(name)
}
