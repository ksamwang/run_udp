package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"udp_tunnel_demo/internal/lan"
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
	fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("%s version=%s commit=%s build_time=%s\n", lan.ServiceName, Version, Commit, BuildTime)
		return
	}

	log.Printf("%s is a placeholder entrypoint for the virtual LAN product line", lan.ServiceName)
	log.Printf("service=%s tray=%q", lan.ServiceName, lan.TrayName)
	log.Printf("config=%s server_http=%s version=%s commit=%s build_time=%s", *configPath, *serverHTTP, Version, Commit, BuildTime)
	log.Printf("virtual LAN runtime is not implemented yet; see task.md")
}
