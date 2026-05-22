package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"udp_tunnel_demo/internal/lan"
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
	wintunPOC := fs.Bool("wintun-poc", false, "create/configure Wintun adapter and wait briefly")
	wintunIP := fs.String("wintun-ip", "172.16.10.250", "Wintun PoC IPv4 address")
	wintunCIDR := fs.String("wintun-cidr", "172.16.10.0/24", "Wintun PoC IPv4 route CIDR")
	wintunMTU := fs.Int("wintun-mtu", wintun.DefaultMTU, "Wintun PoC MTU")
	fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("%s version=%s commit=%s build_time=%s\n", lan.ServiceName, Version, Commit, BuildTime)
		return
	}

	log.Printf("%s is a placeholder entrypoint for the virtual LAN product line", lan.ServiceName)
	log.Printf("service=%s tray=%q", lan.ServiceName, lan.TrayName)
	log.Printf("config=%s server_http=%s version=%s commit=%s build_time=%s", *configPath, *serverHTTP, Version, Commit, BuildTime)
	log.Printf("device_id=%s", lan.DeviceID())
	identity, err := lan.LoadOrCreateIdentity(*configPath)
	if err != nil {
		log.Fatalf("LAN identity failed: %v", err)
	}
	log.Printf("lan_identity_algorithm=%s public_key=%s", identity.Algorithm, identity.PublicKey)
	if *wintunPOC {
		if err := runWintunPOC(*wintunIP, *wintunCIDR, *wintunMTU); err != nil {
			log.Fatalf("Wintun PoC failed: %v", err)
		}
	}
	log.Printf("virtual LAN runtime is not implemented yet; see task.md")
}

func runWintunPOC(ip, cidr string, mtu int) error {
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
	return nil
}
