package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
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
	if cfg.DeviceID == "" {
		cfg.DeviceID = defaultDeviceID()
	}

	if cfg.Server == "" {
		log.Fatal("usage: -server <ip:port> [-probe] | -id <me> -peer <other> [-forward LOCAL:HOST:PORT ...]")
	}
	if *probe {
		runProbe(cfg.Server, *altPort, cfg.PSK, cfg.AllowLegacy)
		return
	}
	if *agent || (cfg.ServerHTTP != "" && cfg.DeviceID != "" && cfg.PeerID == "") {
		runAgent(cfg, *configPath)
		return
	}
	if cfg.DeviceID == "" || cfg.PeerID == "" {
		log.Fatal("usage: -server <ip:port> -id <me> -peer <other> [-forward LOCAL:HOST:PORT ...]")
	}
	for {
		err := runTunnelOnce(cfg, cfg.PeerID, parseForwardRules(cfg.Forwards, cfg.DeviceID, cfg.PeerID))
		log.Printf("[%s] tunnel stopped: %v", cfg.DeviceID, err)
		time.Sleep(2 * time.Second)
	}
}

func runAgent(cfg config.Client, configPath string) {
	if cfg.ServerHTTP == "" {
		cfg.ServerHTTP = udpToHTTP(cfg.Server)
	}
	configURL := startClientConfigServer(&cfg, configPath)
	log.Printf("[%s] agent starting (control=%s tray=%v)", cfg.DeviceID, cfg.ServerHTTP, cfg.TrayEnabled)
	if cfg.TrayEnabled {
		go runTray(cfg.DeviceID, cfg.ServerHTTP, configURL, func() { os.Exit(0) })
	}
	if err := agentPost(cfg, "/api/agent/register", map[string]string{"device_id": cfg.DeviceID, "name": cfg.DeviceID}); err != nil {
		log.Printf("[%s] agent register failed: %v", cfg.DeviceID, err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	var running atomic.Bool
	var currentPeer atomic.Value
	for {
		select {
		case <-stop:
			log.Printf("[%s] signal received, shutting down", cfg.DeviceID)
			return
		default:
		}
		_ = agentPost(cfg, "/api/agent/heartbeat", map[string]string{"device_id": cfg.DeviceID})
		rules, err := agentRules(cfg)
		if err != nil {
			log.Printf("[%s] pull rules failed: %v", cfg.DeviceID, err)
			time.Sleep(5 * time.Second)
			continue
		}
		peer, localRules := selectPeerRules(cfg.DeviceID, rules)
		if peer != "" && !running.Load() {
			running.Store(true)
			currentPeer.Store(peer)
			go func(peer string, rules []forward.Rule) {
				for {
					err := runTunnelOnce(cfg, peer, rules)
					log.Printf("[%s] agent tunnel peer=%s stopped: %v", cfg.DeviceID, peer, err)
					time.Sleep(3 * time.Second)
				}
			}(peer, localRules)
		}
		if peer != "" && currentPeer.Load() != peer {
			log.Printf("[%s] rule peer changed to %s; restart client to switch active peer", cfg.DeviceID, peer)
		}
		time.Sleep(10 * time.Second)
	}
}

func runTunnelOnce(cfg config.Client, peerID string, rules []forward.Rule) error {
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
		return err
	}
	defer conn.Close()
	log.Printf("[%s] local socket: %s", cfg.DeviceID, conn.LocalAddr())

	var upnpMapping *upnp.Mapping
	var upnpAddrStr string
	if !cfg.NoUPnP {
		localPort := conn.LocalAddr().(*net.UDPAddr).Port
		ctx, cancel := context.WithTimeout(context.Background(), cfg.UPnPTimeout+time.Second)
		m, uerr := upnp.Try(ctx, localPort, fmt.Sprintf("udp-tunnel %s", cfg.DeviceID), cfg.UPnPTimeout)
		cancel()
		if uerr != nil {
			log.Printf("[%s] UPnP mapping failed: %v", cfg.DeviceID, uerr)
		} else {
			upnpMapping = m
			upnpAddrStr = m.External()
			log.Printf("[%s] UPnP mapped: %s -> :%d", cfg.DeviceID, upnpAddrStr, m.InternalPort)
			defer upnpMapping.Close()
		}
	}

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
		return writeControl(srvAddr, &protocol.Message{Type: protocol.MsgRegister, From: cfg.DeviceID, Peer: peerID, UpnpAddr: upnpAddrStr})
	}
	if err := register(); err != nil {
		return err
	}
	log.Printf("[%s] registered to server, waiting for peer info...", cfg.DeviceID)

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
			log.Printf("[%s] relay mode (%s), peerAddr -> %s", cfg.DeviceID, reason, srvAddr)
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
					log.Printf("bad peer addr: %v", err)
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
					log.Printf("[%s] hole punched via incoming punch: %s", cfg.DeviceID, src)
					if closeOnce.done.CompareAndSwap(false, true) {
						close(punchedCh)
					}
				}
			case protocol.MsgPunchAck:
				rxCount.Add(1)
				adoptPeerAddrSafe(&peerAddr, src, cfg.DeviceID, &isRelay)
				if punched.CompareAndSwap(false, true) {
					log.Printf("[%s] hole punched via ack: %s", cfg.DeviceID, src)
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
		for range t.C {
			if paddr := peerAddr.Load(); punched.Load() && paddr != nil {
				_ = writeControl(paddr, &protocol.Message{Type: protocol.MsgKeepAlive, From: cfg.DeviceID})
			}
		}
	}()

	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for range t.C {
			if punched.Load() {
				return
			}
			if err := register(); err != nil {
				log.Printf("[%s] register refresh failed: %v", cfg.DeviceID, err)
			}
		}
	}()

	if cfg.ForceRelay {
		enterRelay("-force-relay")
	}
	go func() {
		t := time.NewTimer(cfg.PunchTimeout)
		defer t.Stop()
		select {
		case <-punchedCh:
		case <-t.C:
			enterRelay(fmt.Sprintf("punch timeout %s", cfg.PunchTimeout))
		}
	}()
	<-punchedCh

	isListener := cfg.DeviceID < peerID
	role := "dialer"
	if isListener {
		role = "listener"
	}
	log.Printf("[%s] opening KCP tunnel as %s conv=%d", cfg.DeviceID, role, secure.ConvID(cfg.PSK, cfg.DeviceID, peerID))
	time.Sleep(300 * time.Millisecond)

	kcpConn, err := tunnel.Open(pc, isListener, secure.ConvID(cfg.PSK, cfg.DeviceID, peerID))
	if err != nil {
		return err
	}
	defer kcpConn.Close()
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
	go forward.RunEgress(mux)
	if len(rules) > 0 {
		if err := forward.RunIngress(mux, rules); err != nil {
			return err
		}
	} else {
		log.Printf("[%s] no local forward rules; egress only", cfg.DeviceID)
	}
	<-mux.CloseChan()
	return io.EOF
}

func agentPost(cfg config.Client, path string, body any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.ServerHTTP, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-UDP-Tunnel-PSK", cfg.PSK)
	res, err := http.DefaultClient.Do(req)
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
	res, err := http.DefaultClient.Do(req)
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

func selectPeerRules(deviceID string, rules []store.ForwardRule) (string, []forward.Rule) {
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if r.SourceID == deviceID {
			return r.TargetID, []forward.Rule{{LocalPort: r.LocalPort, Target: fmt.Sprintf("%s:%d", r.TargetHost, r.TargetPort), Name: r.Name}}
		}
		if r.TargetID == deviceID {
			return r.SourceID, nil
		}
	}
	return "", nil
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
	host, _, err := net.SplitHostPort(server)
	if err != nil {
		log.Fatalf("bad server: %v", err)
	}
	primary, _ := net.ResolveUDPAddr("udp", server)
	alt, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, altPort))
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	var codec *secure.Codec
	if psk != "" {
		codec, _ = secure.NewCodec(psk)
	}
	ask := func(target *net.UDPAddr, label string) string {
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
				log.Fatalf("no reply from %s (%s): %v", target, label, err)
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
				return msg.Addr
			}
		}
	}
	a1 := ask(primary, "probe#1")
	a2 := ask(alt, "probe#2")
	_, p1, _ := net.SplitHostPort(a1)
	_, p2, _ := net.SplitHostPort(a2)
	fmt.Println("========== NAT 探测结果 ==========")
	fmt.Printf("主端口观察到: %s\n备端口观察到: %s\n", a1, a2)
	if p1 == p2 {
		fmt.Println("类型判定: Cone 类 NAT")
	} else {
		fmt.Println("类型判定: Symmetric NAT")
	}
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

func defaultDeviceID() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "windows-client"
	}
	return strings.TrimSpace(name)
}
