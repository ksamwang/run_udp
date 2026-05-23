package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"udp_tunnel_demo/internal/lan"
	"udp_tunnel_demo/internal/store"
	"udp_tunnel_demo/internal/wintun"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	fs := flag.NewFlagSet("UDPTunnelLAN", flag.ExitOnError)
	showVersion := fs.Bool("version", false, "print version and exit")
	configPath := fs.String("config", "lan.json", "LAN client config file")
	serverHTTP := fs.String("server-http", "", "control plane HTTP URL")
	applyLANConfig := fs.String("apply-lan-config", "", "internal elevated LAN config source")
	serviceMode := fs.Bool("service", false, "run as Windows service")
	trayMode := fs.Bool("tray", false, "run tray helper")
	installService := fs.Bool("install-service", false, "install Windows service")
	uninstallService := fs.Bool("uninstall-service", false, "uninstall Windows service")
	startServiceFlag := fs.Bool("start-service", false, "start Windows service")
	stopServiceFlag := fs.Bool("stop-service", false, "stop Windows service")
	restartServiceFlag := fs.Bool("restart-service", false, "restart Windows service")
	wintunPOC := fs.Bool("wintun-poc", false, "create/configure Wintun adapter and wait briefly")
	wintunIP := fs.String("wintun-ip", "172.16.10.250", "Wintun PoC IPv4 address")
	wintunCIDR := fs.String("wintun-cidr", "172.16.10.0/24", "Wintun PoC IPv4 route CIDR")
	wintunMTU := fs.Int("wintun-mtu", wintun.DefaultMTU, "Wintun PoC MTU")
	fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("%s version=%s commit=%s build_time=%s\n", lan.ServiceName, Version, Commit, BuildTime)
		return
	}

	configAbs := resolveConfigPath(*configPath)
	if *applyLANConfig != "" {
		next, err := lan.LoadConfig(*applyLANConfig)
		if err != nil {
			log.Fatal(err)
		}
		if err := lan.SaveConfig(configAbs, next); err != nil {
			log.Fatal(err)
		}
		_ = os.Remove(*applyLANConfig)
		return
	}
	cfg, err := lan.LoadConfig(configAbs)
	if err != nil {
		log.Fatal(err)
	}
	if *serverHTTP != "" {
		cfg.ServerHTTP = *serverHTTP
	}
	if *installService || *uninstallService || *startServiceFlag || *stopServiceFlag || *restartServiceFlag {
		exePath := currentExePath()
		switch {
		case *installService:
			must(installWindowsService(exePath, configAbs))
			must(ensureTrayStartup(exePath, configAbs))
		case *uninstallService:
			_ = stopWindowsService()
			must(uninstallWindowsService())
			must(removeTrayStartup())
		case *startServiceFlag:
			must(startWindowsService())
		case *stopServiceFlag:
			must(stopWindowsService())
		case *restartServiceFlag:
			must(restartWindowsService())
		}
		return
	}

	logPath := setupLogging()
	if *trayMode {
		logStartup(configAbs, cfg.ServerHTTP, logPath, "tray")
		configURL := startLANConfigServer(&cfg, configAbs, lanConfigHooks{
			Runtime:        currentLANRuntimeInfo,
			SaveConfig:     func(next lan.Config) (bool, error) { return saveLANConfigWithElevation(configAbs, next) },
			RestartService: restartWindowsServiceWithElevation,
		})
		if configURL != "" {
			openBrowser(configURL)
		}
		runTray(lan.DeviceID(), cfg.ServerHTTP, configURL, trayActions{
			OpenConfig: func() { openBrowser(configURL) },
			OpenLogs:   openLogs,
			Restart:    restartWindowsServiceWithElevation,
		}, func() {})
		return
	}

	run := func(ctx context.Context) {
		logStartup(configAbs, cfg.ServerHTTP, logPath, modeName(*serviceMode))
		if err := runLAN(ctx, configAbs, cfg, *wintunPOC, *wintunIP, *wintunCIDR, *wintunMTU); err != nil {
			log.Printf("LAN runtime stopped: %v", err)
		}
	}
	if *serviceMode {
		must(runWindowsService(run))
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	run(ctx)
}

func runLAN(ctx context.Context, configPath string, cfg lan.Config, wintunPOC bool, wintunIP, wintunCIDR string, wintunMTU int) error {
	log.Printf("%s runtime starting", lan.ServiceName)
	log.Printf("service=%s tray=%q", lan.ServiceName, lan.TrayName)
	log.Printf("config=%s server_http=%s version=%s commit=%s build_time=%s", configPath, cfg.ServerHTTP, Version, Commit, BuildTime)
	log.Printf("device_id=%s", lan.DeviceID())
	identity, err := lan.LoadOrCreateIdentity(configPath)
	if err != nil {
		return fmt.Errorf("LAN identity failed: %w", err)
	}
	log.Printf("lan_identity_algorithm=%s public_key=%s", identity.Algorithm, identity.PublicKey)
	if strings.TrimSpace(cfg.ServerHTTP) == "" {
		log.Printf("LAN bootstrap skipped: server_http is empty; open LAN tray settings to configure it")
	} else {
		if err := bootstrapAndReport(ctx, cfg, identity); err != nil {
			log.Printf("LAN bootstrap failed: %v", err)
		}
	}
	if wintunPOC {
		if err := runWintunPOC(wintunIP, wintunCIDR, wintunMTU); err != nil {
			return fmt.Errorf("Wintun PoC failed: %w", err)
		}
	}
	log.Printf("virtual LAN runtime is not implemented yet; service remains alive for installer/runtime validation")
	<-ctx.Done()
	log.Printf("LAN runtime shutdown requested")
	return nil
}

func bootstrapAndReport(ctx context.Context, cfg lan.Config, identity lan.Identity) error {
	deviceID := lan.DeviceID()
	resp, err := requestLANBootstrap(ctx, cfg.ServerHTTP, lanBootstrapRequest{
		DeviceID: deviceID, DeviceName: defaultDeviceName(), PublicKey: identity.PublicKey,
		Capabilities: []string{"ipv4", "tcp", "rdp", "wintun"},
	})
	if err != nil {
		return err
	}
	log.Printf("LAN bootstrap ok: version=%d config_version=%q network=%s cidr=%s enabled=%v address=%s peers=%d acl=%d routes=%d",
		resp.Version, resp.ConfigVersion, resp.Network.Name, resp.Network.CIDR, resp.Network.Enabled,
		valueOrDash(resp.Address.VirtualIP), len(resp.Peers), len(resp.ACL), len(resp.Routes))
	if resp.Address.VirtualIP == "" {
		log.Printf("LAN virtual IP is not assigned yet; configure it in admin console")
	}
	if len(resp.Peers) == 0 {
		log.Printf("LAN peer list is empty; no other virtual IP is currently assigned in this network")
	}
	adapterState := "not_configured"
	lastError := ""
	selectedCIDR := resp.Network.CIDR
	mtu := wintun.DefaultMTU
	mss := 1200
	if resp.Address.VirtualIP != "" {
		state, err := configureLANAdapter(resp.Address.VirtualIP, resp.Network.CIDR, mtu)
		if err != nil {
			adapterState = "error"
			lastError = err.Error()
			log.Printf("LAN adapter configure failed: %v", err)
		} else {
			adapterState = "up"
			selectedCIDR = state.SelectedCIDR
			mss = state.MSS
			log.Printf("LAN adapter up: name=%q ip=%s cidr=%s mtu=%d mss=%d route_conflict=%v",
				wintun.DefaultAdapterName, resp.Address.VirtualIP, selectedCIDR, mtu, mss, state.Conflict.Conflicts)
		}
	}
	state := store.VirtualPeerState{
		DeviceID: deviceID, NetworkID: resp.Network.ID, State: "bootstrap", AdapterState: adapterState,
		SelectedCIDR: selectedCIDR, MTU: mtu, MSS: mss,
		LastError: lastError, LastTransitionAt: time.Now().Format(time.RFC3339),
	}
	if err := reportLANStatus(ctx, cfg.ServerHTTP, state); err != nil {
		return err
	}
	log.Printf("LAN status reported: state=%s adapter=%s selected_cidr=%s", state.State, state.AdapterState, state.SelectedCIDR)
	return nil
}

func configureLANAdapter(virtualIP, cidr string, mtu int) (wintun.SystemState, error) {
	state, err := wintun.InspectSystem(cidr, mtu)
	if err != nil {
		return state, err
	}
	selectedCIDR := state.SelectedCIDR
	if selectedCIDR == "" {
		selectedCIDR = cidr
	}
	adapter, err := wintun.OpenOrCreate(wintun.Config{
		Name: wintun.DefaultAdapterName,
		IP:   net.ParseIP(virtualIP),
		CIDR: selectedCIDR,
		MTU:  mtu,
	})
	if err != nil {
		return state, err
	}
	if err := adapter.Configure(wintun.Config{
		Name: wintun.DefaultAdapterName,
		IP:   net.ParseIP(virtualIP),
		CIDR: selectedCIDR,
		MTU:  mtu,
	}); err != nil {
		_ = adapter.Close()
		return state, err
	}
	return state, nil
}

func runWintunPOC(ip, cidr string, mtu int) error {
	state, err := wintun.InspectSystem(cidr, mtu)
	if err != nil {
		return err
	}
	if state.Conflict.Conflicts {
		log.Printf("route conflict detected: requested=%s existing=%s interface=%s selected=%s",
			cidr, state.Conflict.Existing.CIDR, state.Conflict.Existing.Interface, state.SelectedCIDR)
		cidr = state.SelectedCIDR
	} else {
		log.Printf("route conflict check passed: cidr=%s mss=%d", cidr, state.MSS)
	}
	adapter, err := wintun.OpenOrCreate(wintun.Config{
		Name: wintun.DefaultAdapterName,
		IP:   net.ParseIP(ip),
		CIDR: cidr,
		MTU:  mtu,
	})
	if err != nil {
		return err
	}
	defer adapter.Close()
	if err := adapter.Configure(wintun.Config{
		Name: wintun.DefaultAdapterName,
		IP:   net.ParseIP(ip),
		CIDR: cidr,
		MTU:  mtu,
	}); err != nil {
		return err
	}
	log.Printf("Wintun PoC ready: adapter=%q ip=%s cidr=%s mtu=%d", wintun.DefaultAdapterName, ip, cidr, mtu)
	time.Sleep(3 * time.Second)
	return wintun.Cleanup(wintun.Config{Name: wintun.DefaultAdapterName, CIDR: cidr})
}

func defaultDeviceName() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return lan.DeviceID()
	}
	return strings.TrimSpace(name)
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return strings.TrimSpace(value)
}

func setupLogging() string {
	dir, err := exeDir()
	if err != nil {
		dir = "."
	}
	logPath := filepath.Join(dir, "UDPTunnelLAN.log")
	prevPath := logPath + ".1"
	if _, err := os.Stat(logPath); err == nil {
		_ = os.Remove(prevPath)
		_ = os.Rename(logPath, prevPath)
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("open log file failed: %v", err)
		return logPath
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	setRuntimeLogPath(logPath)
	return logPath
}

func logStartup(configPath, serverHTTP, logPath, mode string) {
	log.Printf("====== %s startup ======", lan.ServiceName)
	log.Printf("mode=%s os=%s/%s config=%s log=%s", mode, runtime.GOOS, runtime.GOARCH, configPath, logPath)
	log.Printf("server_http=%q version=%s commit=%s build_time=%s", serverHTTP, Version, Commit, BuildTime)
}

func currentLANRuntimeInfo() lanRuntimeInfo {
	installPath := ""
	if dir, err := exeDir(); err == nil {
		installPath = dir
	}
	status, _ := queryWindowsServiceStatus()
	return lanRuntimeInfo{
		Version: Version, Commit: Commit, BuildTime: BuildTime,
		InstallPath: installPath, LogPath: runtimeLogPath(), ServiceStatus: status,
	}
}

func modeName(service bool) string {
	if service {
		return "service"
	}
	return "interactive"
}

func resolveConfigPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if dir, err := exeDir(); err == nil {
		return filepath.Join(dir, path)
	}
	return path
}

func currentExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	return exe
}

func exeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
