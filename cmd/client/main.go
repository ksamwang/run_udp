package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	Peer             string `json:"peer"`
	Profile          string `json:"profile"`
	State            string `json:"state"`
	Via              string `json:"via"`
	PublicAddr       string `json:"public_addr"`
	ConvID           int64  `json:"conv_id"`
	RTTMs            int    `json:"rtt_ms"`
	NATType          string `json:"nat_type"`
	LastError        string `json:"last_error"`
	Attempt          int    `json:"attempt"`
	NextRetryAt      string `json:"next_retry_at"`
	LastTransitionAt string `json:"last_transition_at"`
}

type agentPeerSession struct {
	peer       string
	profile    string
	sig        string
	ruleIDs    []int64
	localPorts []int

	cancel context.CancelFunc
	done   chan struct{}
	wake   chan string

	mu           sync.RWMutex
	status       agentTunnelStatus
	addr         string
	upnp         string
	tunnelCancel context.CancelFunc
}

type peerRuleSet struct {
	Peer       string
	Profile    string
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

type natRuntime struct {
	mu     sync.RWMutex
	result natProbeResult
}

type releaseInfo struct {
	Version                 string `json:"version"`
	URL                     string `json:"url"`
	SHA256                  string `json:"sha256"`
	PublishedAt             string `json:"published_at"`
	Notes                   string `json:"notes"`
	MinimumSupportedVersion string `json:"minimum_supported_version"`
}

type updateManager struct {
	mu      sync.Mutex
	trigger chan string
}

var (
	backoffRandMu sync.Mutex
	backoffRand   = mrand.New(mrand.NewSource(time.Now().UnixNano()))
)

type bootstrapResponse struct {
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

func (s *agentPeerSession) setStatus(st agentTunnelStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.LastTransitionAt == "" {
		st.LastTransitionAt = time.Now().Format(time.RFC3339)
	}
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

func (s *agentPeerSession) setTunnelCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tunnelCancel = cancel
}

func (s *agentPeerSession) clearTunnelCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fmt.Sprintf("%p", s.tunnelCancel) == fmt.Sprintf("%p", cancel) {
		s.tunnelCancel = nil
	}
}

func (s *agentPeerSession) trigger(reason string) {
	s.mu.RLock()
	cancel := s.tunnelCancel
	s.mu.RUnlock()
	select {
	case s.wake <- reason:
	default:
	}
	if cancel != nil {
		cancel()
	}
}

func (n *natRuntime) Get() natProbeResult {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.result
}

func (n *natRuntime) Set(res natProbeResult) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.result = res
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
	trayMode := fs.Bool("tray", false, "run tray helper only")
	serviceMode := fs.Bool("service", false, "run as Windows service")
	installService := fs.Bool("install-service", false, "install Windows service")
	uninstallService := fs.Bool("uninstall-service", false, "uninstall Windows service")
	startServiceFlag := fs.Bool("start-service", false, "start Windows service")
	stopServiceFlag := fs.Bool("stop-service", false, "stop Windows service")
	restartServiceFlag := fs.Bool("restart-service", false, "restart Windows service")
	checkUpdatesFlag := fs.Bool("check-updates", false, "check for updates immediately")
	updaterMode := fs.Bool("updater", false, "internal updater helper mode")
	updatePackage := fs.String("update-package", "", "installer package path for updater helper")
	applyClientConfig := fs.String("apply-client-config", "", "internal elevated client config source")
	var forwards multiFlag
	fs.Var(&forwards, "forward", "TCP forward rule LOCAL:HOST:PORT")
	fs.Parse(os.Args[1:])

	if *applyClientConfig != "" {
		next := config.DefaultClient()
		if err := config.LoadJSON(*applyClientConfig, &next); err != nil {
			log.Fatal(err)
		}
		if err := config.SaveClientLocalJSON(*configPath, next); err != nil {
			log.Fatal(err)
		}
		_ = os.Remove(*applyClientConfig)
		return
	}

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
	if *installService || *uninstallService || *startServiceFlag || *stopServiceFlag || *restartServiceFlag {
		exePath := currentExePath()
		configAbs := *configPath
		if !filepath.IsAbs(configAbs) {
			if dir, err := exeDir(); err == nil {
				configAbs = filepath.Join(dir, configAbs)
			}
		}
		switch {
		case *installService:
			if err := installWindowsService(exePath, configAbs); err != nil {
				log.Fatal(err)
			}
			if err := ensureTrayStartup(exePath, configAbs); err != nil {
				log.Fatal(err)
			}
			return
		case *uninstallService:
			_ = removeTrayStartup()
			if err := uninstallWindowsService(); err != nil {
				log.Fatal(err)
			}
			return
		case *startServiceFlag:
			if err := startWindowsService(); err != nil {
				log.Fatal(err)
			}
			return
		case *stopServiceFlag:
			if err := stopWindowsService(); err != nil {
				log.Fatal(err)
			}
			return
		case *restartServiceFlag:
			if err := restartWindowsService(); err != nil {
				log.Fatal(err)
			}
			return
		}
	}
	if err := ensureLocalIdentity(&cfg, *configPath); err != nil {
		log.Fatal(err)
	}
	logPath := setupLogging(cfg.DeviceID)
	logStartup(cfg, *configPath, logPath, *agent, *probe)
	if st, err := queryWindowsServiceStatus(); err == nil {
		appRuntime.SetServiceStatus(st)
	}
	if *serviceMode {
		if err := runWindowsService(func(ctx context.Context) {
			runServiceAgent(ctx, cfg, *configPath, *altPort)
		}); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *updaterMode {
		if err := runUpdaterHelper(*updatePackage); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *trayMode {
		runTrayProcess(cfg, *configPath)
		return
	}
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
				if shouldRunBootstrapTray(*agent, cfg) {
					runBootstrapTrayMode(&cfg, *configPath)
					return
				}
				runBootstrapConfigMode(&cfg, *configPath)
				return
			}
		} else {
			if merged, err := mergeBootstrap(cfg, resp); err != nil {
				log.Printf("[%s] bad bootstrap config: %v", cfg.DeviceID, err)
				if shouldRunBootstrapTray(*agent, cfg) {
					runBootstrapTrayMode(&cfg, *configPath)
					return
				}
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
		if shouldRunBootstrapTray(*agent, cfg) {
			runBootstrapTrayMode(&cfg, *configPath)
			return
		}
		runBootstrapConfigMode(&cfg, *configPath)
		return
	}
	if *probe {
		runProbe(runtimeCfg.Server, bootstrapAltPort, runtimeCfg.PSK, runtimeCfg.AllowLegacy)
		return
	}
	natResult := autoDetectNAT(runtimeCfg, bootstrapAltPort)
	if *checkUpdatesFlag {
		if _, err := checkForUpdates(runtimeCfg, *configPath, false); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *agent || (runtimeCfg.ServerHTTP != "" && runtimeCfg.DeviceID != "" && runtimeCfg.PeerID == "") {
		runAgent(cfg, runtimeCfg, *configPath, natResult, bootstrapAltPort)
		return
	}
	if runtimeCfg.DeviceID == "" || runtimeCfg.PeerID == "" {
		log.Fatal("usage: -server <ip:port> or -server-http <url> -id <me> -peer <other> [-forward LOCAL:HOST:PORT ...]")
	}
	for {
		err := runTunnelOnce(context.Background(), runtimeCfg, runtimeCfg.PeerID, store.ProfileInteractive, parseForwardRules(runtimeCfg.Forwards, runtimeCfg.DeviceID, runtimeCfg.PeerID), natResult, func(agentTunnelStatus, string, string) {})
		log.Printf("[%s] tunnel stopped: %v", runtimeCfg.DeviceID, err)
		time.Sleep(2 * time.Second)
	}
}

func runServiceAgent(ctx context.Context, cfg config.Client, configPath string, altPort int) {
	if needsBootstrapConfig(cfg, false, false) {
		log.Printf("[%s] service config incomplete: server_http=%q", cfg.DeviceID, cfg.ServerHTTP)
		<-ctx.Done()
		return
	}

	runtimeCfg := cfg
	bootstrapAltPort := altPort
	if cfg.ServerHTTP != "" {
		if resp, err := agentBootstrap(cfg); err != nil {
			if cfg.Server == "" {
				log.Printf("[%s] bootstrap failed: %v", cfg.DeviceID, err)
				<-ctx.Done()
				return
			}
		} else {
			merged, err := mergeBootstrap(cfg, resp)
			if err != nil {
				log.Printf("[%s] bad bootstrap config: %v", cfg.DeviceID, err)
				<-ctx.Done()
				return
			}
			runtimeCfg = merged
			bootstrapAltPort = resp.STUNAltPort
		}
	}
	if runtimeCfg.DeviceID != cfg.DeviceID {
		log.Printf("[%s] bootstrap updated device_id -> %s", cfg.DeviceID, runtimeCfg.DeviceID)
	}
	if runtimeCfg.Server == "" {
		log.Printf("[%s] runtime server is empty after bootstrap", runtimeCfg.DeviceID)
		<-ctx.Done()
		return
	}

	natResult := autoDetectNAT(runtimeCfg, bootstrapAltPort)
	runAgentLoop(ctx, runtimeCfg, configPath, natResult, bootstrapAltPort, newUpdateManager(runtimeCfg, configPath, true))
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
	return strings.TrimSpace(cfg.ServerHTTP) == ""
}

func shouldRunBootstrapTray(agentMode bool, cfg config.Client) bool {
	return agentMode && cfg.TrayEnabled
}

func runBootstrapConfigMode(cfg *config.Client, configPath string) {
	configURL := startClientConfigServer(cfg, configPath, clientConfigHooks{
		OnSaved: restartSelf,
		Runtime: currentRuntimeInfo,
	})
	if configURL != "" {
		log.Printf("[%s] bootstrap config required; opening local settings page: %s", cfg.DeviceID, configURL)
		openBrowser(configURL)
	} else {
		log.Printf("[%s] bootstrap config required; local settings page could not start", cfg.DeviceID)
	}
	log.Printf("[%s] missing local bootstrap config: server_http=%q", cfg.DeviceID, cfg.ServerHTTP)
	waitForSignal(cfg.DeviceID)
}

func runBootstrapTrayMode(cfg *config.Client, configPath string) {
	configURL := startClientConfigServer(cfg, configPath, clientConfigHooks{
		OnSaved: restartSelf,
		Runtime: currentRuntimeInfo,
	})
	if configURL != "" {
		log.Printf("[%s] bootstrap unavailable; tray settings page is available: %s", cfg.DeviceID, configURL)
		openBrowser(configURL)
	} else {
		log.Printf("[%s] bootstrap unavailable; local settings page could not start", cfg.DeviceID)
	}
	runTray(cfg.DeviceID, cfg.ServerHTTP, configURL, trayActions{
		OpenLogs:     openLogs,
		CheckUpdates: func() error { return fmt.Errorf("bootstrap unavailable") },
		RuntimeStatus: func() string {
			return currentRuntimeInfo().ServiceStatus
		},
	}, func() { os.Exit(0) })
}

func waitForSignal(deviceID string) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Printf("[%s] signal received, shutting down", deviceID)
}

func runAgent(localCfg, runtimeCfg config.Client, configPath string, natResult natProbeResult, altPort int) {
	if runtimeCfg.ServerHTTP == "" {
		runtimeCfg.ServerHTTP = udpToHTTP(runtimeCfg.Server)
	}
	configURL := startClientConfigServer(&localCfg, configPath, clientConfigHooks{
		OnSaved: restartSelf,
		Runtime: currentRuntimeInfo,
	})
	log.Printf("[%s] agent starting (control=%s tray=%v name=%q)", runtimeCfg.DeviceID, runtimeCfg.ServerHTTP, runtimeCfg.TrayEnabled, runtimeCfg.DeviceName)
	if runtimeCfg.TrayEnabled {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go runAgentLoop(ctx, runtimeCfg, configPath, natResult, altPort, newUpdateManager(runtimeCfg, configPath, false))
		runTray(runtimeCfg.DeviceID, runtimeCfg.ServerHTTP, configURL, trayActions{
			OpenLogs:     openLogs,
			CheckUpdates: func() error { _, err := checkForUpdates(runtimeCfg, configPath, false); return err },
			RuntimeStatus: func() string {
				return currentRuntimeInfo().ServiceStatus
			},
		}, func() { stop() })
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runAgentLoop(ctx, runtimeCfg, configPath, natResult, altPort, newUpdateManager(runtimeCfg, configPath, false))
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

func runTrayProcess(cfg config.Client, configPath string) {
	configURL := startClientConfigServer(&cfg, configPath, clientConfigHooks{
		OnSaved: func() {
			if err := spawnServiceCommand("-restart-service"); err != nil {
				log.Printf("restart service helper failed: %v", err)
			}
			os.Exit(0)
		},
		SaveConfig:     func(next config.Client) (bool, error) { return saveClientConfigWithElevation(configPath, next) },
		Runtime:        currentRuntimeInfo,
		RestartService: restartWindowsServiceWithElevation,
		CheckUpdates: func() error {
			_, err := checkForUpdates(cfg, configPath, false)
			return err
		},
	})
	runTray(cfg.DeviceID, cfg.ServerHTTP, configURL, trayActions{
		OpenLogs:     openLogs,
		Restart:      restartWindowsServiceWithElevation,
		CheckUpdates: func() error { _, err := checkForUpdates(cfg, configPath, false); return err },
		RuntimeStatus: func() string {
			if st, err := queryWindowsServiceStatus(); err == nil {
				appRuntime.SetServiceStatus(st)
				return st
			}
			return currentRuntimeInfo().ServiceStatus
		},
	}, func() { os.Exit(0) })
}

func newUpdateManager(cfg config.Client, configPath string, serviceMode bool) *updateManager {
	if !serviceMode {
		return nil
	}
	appRuntime.SetUpdateStatus("idle", "", "")
	return &updateManager{trigger: make(chan string, 1)}
}

func (u *updateManager) Trigger(reason string) {
	if u == nil {
		return
	}
	select {
	case u.trigger <- reason:
	default:
	}
}

func runAgentLoop(ctx context.Context, runtimeCfg config.Client, configPath string, natResult natProbeResult, altPort int, updates *updateManager) {
	if err := agentPost(runtimeCfg, "/api/agent/register", map[string]any{
		"device_id": runtimeCfg.DeviceID,
		"name":      runtimeCfg.DeviceName,
		"nat_type":  natResult.NATType,
	}); err != nil {
		log.Printf("[%s] agent register failed: %v", runtimeCfg.DeviceID, err)
	}
	rootCtx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()
	natState := &natRuntime{result: natResult}
	sessions := map[string]*agentPeerSession{}
	var lastEmptyLog time.Time
	var lastRulesSig string
	var lastHeartbeatAddr, lastHeartbeatUPnP string
	rulesTicker := time.NewTicker(5 * time.Second)
	defer rulesTicker.Stop()
	heartbeatTicker := time.NewTicker(10 * time.Second)
	defer heartbeatTicker.Stop()
	var updateTicker *time.Ticker
	var updateTick <-chan time.Time
	var updateTrigger <-chan string
	if updates != nil {
		updateTicker = time.NewTicker(6 * time.Hour)
		defer updateTicker.Stop()
		updateTick = updateTicker.C
		updateTrigger = updates.trigger
	}
	resumeCh := startResumeMonitor(rootCtx)

	refreshNAT := func(reason string) {
		next := autoDetectNAT(runtimeCfg, altPort)
		natState.Set(next)
		log.Printf("[%s] network change reason=%s nat=%s primary=%s alt=%s", runtimeCfg.DeviceID, reason, next.NATType, next.PrimaryAddr, next.AltAddr)
	}
	triggerAllPeers := func(reason string) {
		refreshNAT(reason)
		for key, sess := range sessions {
			log.Printf("[%s] tunnel=%s peer=%s profile=%s trigger immediate reconnect: %s", runtimeCfg.DeviceID, key, sess.peer, sess.profile, reason)
			sess.trigger(reason)
		}
	}
	syncRules := func() {
		rules, err := agentRules(runtimeCfg)
		if err != nil {
			log.Printf("[%s] pull rules failed: %v", runtimeCfg.DeviceID, err)
			return
		}
		sig := rulesSignature(rules)
		if sig != lastRulesSig {
			log.Printf("[%s] rules updated: %d total -> %s", runtimeCfg.DeviceID, len(rules), sig)
			lastRulesSig = sig
		}
		grouped := groupRulesByPeer(runtimeCfg.DeviceID, rules)
		if len(grouped) == 0 && time.Since(lastEmptyLog) > 60*time.Second {
			log.Printf("[%s] no enabled rule references this device (rules=%d); waiting", runtimeCfg.DeviceID, len(rules))
			lastEmptyLog = time.Now()
		}
		for key, sess := range sessions {
			nextSet, ok := grouped[key]
			nextSig := forwardRulesSignature(nextSet)
			if ok && sess.sig == nextSig {
				continue
			}
			log.Printf("[%s] peer=%s profile=%s rules=%v local_ports=%v stopping: rule removed or changed", runtimeCfg.DeviceID, sess.peer, sess.profile, sess.ruleIDs, sess.localPorts)
			sess.setStatus(agentTunnelStatus{Peer: sess.peer, Profile: sess.profile, State: "disabled", LastError: "rule_changed"})
			_ = agentPost(runtimeCfg, "/api/agent/tunnel-status", map[string]any{
				"device_id":  runtimeCfg.DeviceID,
				"peer":       sess.peer,
				"profile":    sess.profile,
				"state":      "disabled",
				"last_error": "rule_changed",
			})
			sess.cancel()
			<-sess.done
			delete(sessions, key)
		}
		for key, ruleSet := range grouped {
			if _, ok := sessions[key]; ok {
				continue
			}
			sctx, scancel := context.WithCancel(rootCtx)
			sess := &agentPeerSession{
				peer:       ruleSet.Peer,
				profile:    ruleSet.Profile,
				sig:        forwardRulesSignature(ruleSet),
				ruleIDs:    append([]int64(nil), ruleSet.RuleIDs...),
				localPorts: append([]int(nil), ruleSet.LocalPorts...),
				cancel:     scancel,
				done:       make(chan struct{}),
				wake:       make(chan string, 1),
			}
			sessions[key] = sess
			log.Printf("[%s] peer=%s profile=%s rule_ids=%v local_ports=%v activating ingress_rules=%d", runtimeCfg.DeviceID, ruleSet.Peer, ruleSet.Profile, ruleSet.RuleIDs, ruleSet.LocalPorts, len(ruleSet.Forward))
			go runPeerWorker(sctx, runtimeCfg, natState, ruleSet.Peer, ruleSet, sess)
		}
	}
	sendHeartbeat := func() {
		addr, upnp, tunnels := snapshotAgentSessions(sessions)
		if addr != "" && lastHeartbeatAddr != "" && addr != lastHeartbeatAddr {
			log.Printf("[%s] local/public addr changed: %s -> %s", runtimeCfg.DeviceID, lastHeartbeatAddr, addr)
			lastHeartbeatAddr = addr
			lastHeartbeatUPnP = upnp
			triggerAllPeers("addr_changed")
		} else {
			if addr != "" {
				lastHeartbeatAddr = addr
			}
			if upnp != "" {
				if lastHeartbeatUPnP != "" && upnp != lastHeartbeatUPnP {
					log.Printf("[%s] upnp/public addr changed: %s -> %s", runtimeCfg.DeviceID, lastHeartbeatUPnP, upnp)
					lastHeartbeatUPnP = upnp
					triggerAllPeers("upnp_changed")
				} else {
					lastHeartbeatUPnP = upnp
				}
			}
		}
		if err := agentPost(runtimeCfg, "/api/agent/heartbeat", map[string]any{
			"device_id": runtimeCfg.DeviceID,
			"name":      runtimeCfg.DeviceName,
			"addr":      addr,
			"upnp_addr": upnp,
			"nat_type":  natState.Get().NATType,
			"tunnels":   tunnels,
		}); err != nil {
			log.Printf("[%s] heartbeat failed: %v", runtimeCfg.DeviceID, err)
		}
	}
	syncRules()
	sendHeartbeat()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[%s] signal received, shutting down", runtimeCfg.DeviceID)
			cancelAll()
			for _, sess := range sessions {
				<-sess.done
			}
			return
		case <-rulesTicker.C:
			syncRules()
		case <-heartbeatTicker.C:
			sendHeartbeat()
		case <-updateTick:
			if _, err := checkForUpdates(runtimeCfg, configPath, false); err != nil {
				log.Printf("[%s] auto update check failed: %v", runtimeCfg.DeviceID, err)
			}
		case reason := <-updateTrigger:
			log.Printf("[%s] update check trigger: %s", runtimeCfg.DeviceID, reason)
			if _, err := checkForUpdates(runtimeCfg, configPath, false); err != nil {
				log.Printf("[%s] manual update check failed: %v", runtimeCfg.DeviceID, err)
			}
		case reason := <-resumeCh:
			log.Printf("[%s] system/network event: %s", runtimeCfg.DeviceID, reason)
			triggerAllPeers(reason)
		}
	}
}

func runTunnelOnce(ctx context.Context, cfg config.Client, peerID, profile string, rules []forward.Rule, natResult natProbeResult, report func(agentTunnelStatus, string, string)) error {
	profile = store.NormalizeProfile(profile)
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
	log.Printf("[%s] peer=%s profile=%s local socket=%s", cfg.DeviceID, peerID, profile, conn.LocalAddr())

	var upnpMapping *upnp.Mapping
	var upnpAddrStr string
	if !cfg.NoUPnP {
		localPort := conn.LocalAddr().(*net.UDPAddr).Port
		ctx, cancel := context.WithTimeout(context.Background(), cfg.UPnPTimeout+time.Second)
		m, uerr := upnp.Try(ctx, localPort, fmt.Sprintf("udp-tunnel %s", cfg.DeviceID), cfg.UPnPTimeout)
		cancel()
		if uerr != nil {
			log.Printf("[%s] peer=%s profile=%s upnp failed: %v", cfg.DeviceID, peerID, profile, uerr)
		} else {
			upnpMapping = m
			upnpAddrStr = m.External()
			log.Printf("[%s] peer=%s profile=%s upnp mapped: %s -> :%d", cfg.DeviceID, peerID, profile, upnpAddrStr, m.InternalPort)
			defer upnpMapping.Close()
			go refreshUPnPMapping(ctx, cfg.DeviceID, upnpMapping)
		}
	}
	report(agentTunnelStatus{Peer: peerID, Profile: profile, State: "connecting", NATType: natResult.NATType}, "", upnpAddrStr)

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
		return writeControl(srvAddr, &protocol.Message{Type: protocol.MsgRegister, From: cfg.DeviceID, Name: cfg.DeviceName, Peer: peerID, Profile: profile, UpnpAddr: upnpAddrStr})
	}
	if err := register(); err != nil {
		return fmt.Errorf("register_failed: %w", err)
	}
	log.Printf("[%s] peer=%s profile=%s registered to server, waiting for peer info...", cfg.DeviceID, peerID, profile)

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
			log.Printf("[%s] peer=%s profile=%s relay mode (%s), peerAddr -> %s", cfg.DeviceID, peerID, profile, reason, srvAddr)
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
				if store.NormalizeProfile(msg.Profile) != profile {
					continue
				}
				if isRelay.Load() {
					continue
				}
				paddr, err := net.ResolveUDPAddr("udp", msg.Addr)
				if err != nil {
					log.Printf("[%s] peer=%s profile=%s bad peer addr: %v", cfg.DeviceID, peerID, profile, err)
					continue
				}
				peerAddr.Store(paddr)
				if msg.UpnpAddr != "" {
					if uaddr, uerr := net.ResolveUDPAddr("udp", msg.UpnpAddr); uerr == nil {
						peerUpnpAddr.Store(uaddr)
					}
				}
				if punchStarted.CompareAndSwap(false, true) {
					go punchLoop(conn, &peerAddr, &peerUpnpAddr, &punched, cfg.DeviceID, profile, writeControl)
				}
			case protocol.MsgPunch:
				if store.NormalizeProfile(msg.Profile) != profile {
					continue
				}
				rxCount.Add(1)
				adoptPeerAddrSafe(&peerAddr, src, cfg.DeviceID, &isRelay)
				_ = writeControl(src, &protocol.Message{Type: protocol.MsgPunchAck, From: cfg.DeviceID, Profile: profile})
				if punched.CompareAndSwap(false, true) {
					log.Printf("[%s] peer=%s profile=%s hole punched via incoming punch: %s", cfg.DeviceID, peerID, profile, src)
					if closeOnce.done.CompareAndSwap(false, true) {
						close(punchedCh)
					}
				}
			case protocol.MsgPunchAck:
				if store.NormalizeProfile(msg.Profile) != profile {
					continue
				}
				rxCount.Add(1)
				adoptPeerAddrSafe(&peerAddr, src, cfg.DeviceID, &isRelay)
				if punched.CompareAndSwap(false, true) {
					log.Printf("[%s] peer=%s profile=%s hole punched via ack: %s", cfg.DeviceID, peerID, profile, src)
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
				_ = writeControl(paddr, &protocol.Message{Type: protocol.MsgKeepAlive, From: cfg.DeviceID, Profile: profile})
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
	convID := secure.ConvID(cfg.PSK, cfg.DeviceID, peerID, profile)
	log.Printf("[%s] peer=%s profile=%s opening KCP tunnel as %s conv=%d", cfg.DeviceID, peerID, profile, role, convID)
	time.Sleep(300 * time.Millisecond)

	kcpConn, err := tunnel.Open(pc, isListener, convID, profile)
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
	smuxCfg := smuxConfig(profile)
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
		Profile:    profile,
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
					Profile:    profile,
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

func smuxConfig(profile string) *smux.Config {
	cfg := smux.DefaultConfig()
	cfg.Version = 2
	cfg.KeepAliveInterval = 10 * time.Second
	cfg.KeepAliveTimeout = 30 * time.Second
	switch store.NormalizeProfile(profile) {
	case store.ProfileBulk:
		cfg.MaxStreamBuffer = 16 * 1024 * 1024
		cfg.MaxReceiveBuffer = 64 * 1024 * 1024
	default:
		cfg.MaxStreamBuffer = 512 * 1024
		cfg.MaxReceiveBuffer = 8 * 1024 * 1024
	}
	return cfg
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

// groupRulesByPeer 把控制面下发的所有规则按对端 ID + profile 分组：
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
		profile := store.NormalizeProfile(r.Profile)
		switch deviceID {
		case r.SourceID:
			key := tunnelGroupKey(r.TargetID, profile)
			set := grouped[key]
			set.Peer = r.TargetID
			set.Profile = profile
			set.Forward = append(set.Forward, forward.Rule{
				LocalPort: r.LocalPort,
				Target:    fmt.Sprintf("%s:%d", r.TargetHost, r.TargetPort),
				Name:      r.Name,
				Profile:   profile,
			})
			set.RuleIDs = append(set.RuleIDs, r.ID)
			set.LocalPorts = append(set.LocalPorts, r.LocalPort)
			grouped[key] = set
		case r.TargetID:
			key := tunnelGroupKey(r.SourceID, profile)
			if _, ok := grouped[key]; !ok {
				grouped[key] = peerRuleSet{Peer: r.SourceID, Profile: profile}
			}
		}
	}
	return grouped
}

func tunnelGroupKey(peer, profile string) string {
	return peer + "\x00" + store.NormalizeProfile(profile)
}

func forwardRulesSignature(rules peerRuleSet) string {
	parts := make([]string, 0, len(rules.Forward))
	for i, r := range rules.Forward {
		ruleID := int64(0)
		if i < len(rules.RuleIDs) {
			ruleID = rules.RuleIDs[i]
		}
		parts = append(parts, fmt.Sprintf("#%d:%s:%s:%d->%s", ruleID, r.Profile, r.Name, r.LocalPort, r.Target))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func snapshotAgentSessions(sessions map[string]*agentPeerSession) (string, string, []agentTunnelStatus) {
	keys := make([]string, 0, len(sessions))
	for key := range sessions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]agentTunnelStatus, 0, len(keys))
	var addr, upnp string
	for _, key := range keys {
		sess := sessions[key]
		st, a, u := sess.snapshot()
		if st.Peer == "" {
			st.Peer = sess.peer
		}
		if st.Profile == "" {
			st.Profile = sess.profile
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

func runPeerWorker(ctx context.Context, runtimeCfg config.Client, natState *natRuntime, peer string, rules peerRuleSet, st *agentPeerSession) {
	defer close(st.done)
	profile := store.NormalizeProfile(rules.Profile)
	attempt := 0
	report := func(ts agentTunnelStatus, addr, upnp string) {
		ts.Peer = peer
		ts.Profile = profile
		st.setStatus(ts)
		st.setDeviceAddrs(addr, upnp)
		if ts.State == "" {
			return
		}
		body := map[string]any{
			"device_id":          runtimeCfg.DeviceID,
			"name":               runtimeCfg.DeviceName,
			"peer":               peer,
			"profile":            profile,
			"state":              ts.State,
			"via":                ts.Via,
			"public_addr":        ts.PublicAddr,
			"conv_id":            ts.ConvID,
			"rtt_ms":             ts.RTTMs,
			"nat_type":           ts.NATType,
			"last_error":         ts.LastError,
			"attempt":            ts.Attempt,
			"next_retry_at":      ts.NextRetryAt,
			"last_transition_at": ts.LastTransitionAt,
			"addr":               addr,
			"upnp_addr":          upnp,
		}
		if err := agentPost(runtimeCfg, "/api/agent/tunnel-status", body); err != nil && ctx.Err() == nil {
			log.Printf("[%s] tunnel-status peer=%s profile=%s failed: %v", runtimeCfg.DeviceID, peer, profile, err)
		}
	}
	for {
		if ctx.Err() != nil {
			report(agentTunnelStatus{Peer: peer, Profile: profile, State: "disabled", LastError: "rule_cancelled"}, "", "")
			return
		}
		connected := false
		transitionAt := time.Now().Format(time.RFC3339)
		report(agentTunnelStatus{
			Peer:             peer,
			State:            "connecting",
			Attempt:          attempt,
			LastTransitionAt: transitionAt,
		}, "", "")
		runCtx, runCancel := context.WithCancel(ctx)
		st.setTunnelCancel(runCancel)
		err := runTunnelOnce(runCtx, runtimeCfg, peer, profile, rules.Forward, natState.Get(), func(ts agentTunnelStatus, addr, upnp string) {
			if ts.State == "p2p" || ts.State == "relay" {
				connected = true
				attempt = 0
			}
			if ts.LastTransitionAt == "" {
				ts.LastTransitionAt = time.Now().Format(time.RFC3339)
			}
			report(ts, addr, upnp)
		})
		st.clearTunnelCancel(runCancel)
		if ctx.Err() != nil {
			report(agentTunnelStatus{Peer: peer, Profile: profile, State: "disabled", LastError: "rule_cancelled"}, "", "")
			return
		}
		reason, triggered := drainWakeReason(st.wake)
		if triggered {
			log.Printf("[%s] peer=%s profile=%s rule_ids=%v local_ports=%v immediate reconnect: %s", runtimeCfg.DeviceID, peer, profile, rules.RuleIDs, rules.LocalPorts, reason)
			if reason == "addr_changed" || reason == "upnp_changed" || reason == "system_resume" {
				attempt = 0
			}
			continue
		}
		tunnelReason := classifyTunnelError(err)
		log.Printf("[%s] peer=%s profile=%s rule_ids=%v local_ports=%v tunnel stopped: %s (%v)", runtimeCfg.DeviceID, peer, profile, rules.RuleIDs, rules.LocalPorts, tunnelReason, err)
		if connected {
			attempt = 0
		}
		if !shouldRetryTunnelError(tunnelReason) {
			report(agentTunnelStatus{
				Peer:             peer,
				State:            "down",
				LastError:        tunnelReason,
				Attempt:          attempt,
				LastTransitionAt: time.Now().Format(time.RFC3339),
			}, "", "")
			select {
			case <-ctx.Done():
				report(agentTunnelStatus{Peer: peer, Profile: profile, State: "disabled", LastError: "rule_cancelled"}, "", "")
				return
			case reason := <-st.wake:
				log.Printf("[%s] peer=%s profile=%s wake from down: %s", runtimeCfg.DeviceID, peer, profile, reason)
				if reason == "addr_changed" || reason == "upnp_changed" || reason == "system_resume" {
					attempt = 0
				}
				continue
			}
		}
		attempt++
		delay := nextBackoffDelay(attempt)
		nextRetry := time.Now().Add(delay).Format(time.RFC3339)
		report(agentTunnelStatus{
			Peer:             peer,
			State:            "backoff",
			LastError:        tunnelReason,
			Attempt:          attempt,
			NextRetryAt:      nextRetry,
			LastTransitionAt: time.Now().Format(time.RFC3339),
		}, "", "")
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			report(agentTunnelStatus{Peer: peer, Profile: profile, State: "disabled", LastError: "rule_cancelled"}, "", "")
			return
		case reason := <-st.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			log.Printf("[%s] peer=%s profile=%s wake during backoff: %s", runtimeCfg.DeviceID, peer, profile, reason)
			if reason == "addr_changed" || reason == "upnp_changed" || reason == "system_resume" {
				attempt = 0
			}
		case <-timer.C:
		}
	}
}

func classifyTunnelError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case errors.Is(err, context.Canceled):
		return "rule_cancelled"
	case strings.Contains(msg, "peer_info_timeout"):
		return "peer_info_timeout"
	case strings.Contains(msg, "punch_timeout"):
		return "punch_timeout"
	case strings.Contains(msg, "register_failed"):
		return "register_failed"
	case strings.Contains(msg, "kcp_open_failed"):
		return "kcp_open_failed"
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

func shouldRetryTunnelError(reason string) bool {
	switch reason {
	case "rule_cancelled", "rule_changed", "ingress_listen_failed":
		return false
	default:
		return true
	}
}

func nextBackoffDelay(attempt int) time.Duration {
	steps := []time.Duration{
		time.Second,
		2 * time.Second,
		5 * time.Second,
		10 * time.Second,
		20 * time.Second,
		30 * time.Second,
		60 * time.Second,
	}
	if attempt <= 0 {
		return steps[0]
	}
	idx := attempt - 1
	if idx >= len(steps) {
		idx = len(steps) - 1
	}
	base := steps[idx]
	jitterMin := float64(base) * 0.10
	jitterMax := float64(base) * 0.20
	if jitterMax <= jitterMin {
		return base
	}
	backoffRandMu.Lock()
	jitter := jitterMin + backoffRand.Float64()*(jitterMax-jitterMin)
	backoffRandMu.Unlock()
	return base + time.Duration(jitter)
}

func drainWakeReason(ch <-chan string) (string, bool) {
	select {
	case reason := <-ch:
		return reason, true
	default:
		return "", false
	}
}

func ensureLocalIdentity(cfg *config.Client, configPath string) error {
	changed := false
	if strings.TrimSpace(cfg.DeviceName) == "" {
		cfg.DeviceName = defaultDeviceName()
		changed = true
	}
	if cfg.PeerID == "" {
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
	if id := stableDeviceID(machineUUID()); id != "" {
		return id
	}
	return stableDeviceID(defaultDeviceName())
}

func stableDeviceID(seed string) string {
	seed = strings.ToLower(strings.TrimSpace(seed))
	if seed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(seed))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	return "DEV-" + encoded[:16]
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

var doAgentHTTPRequest = func(req *http.Request) (*http.Response, error) {
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
	cfg.PSK = resp.PSK
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

func punchLoop(conn *net.UDPConn, peerAddr, peerUpnpAddr *atomic.Pointer[net.UDPAddr], punched *atomic.Bool, id, profile string, writeControl func(*net.UDPAddr, *protocol.Message) error) {
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
		msg := &protocol.Message{Type: protocol.MsgPunch, From: id, Profile: profile}
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
