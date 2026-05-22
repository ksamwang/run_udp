package main

import (
	"context"
	"flag"
	"log"
	"os"

	"udp_tunnel_demo/internal/config"
	serverapp "udp_tunnel_demo/internal/server"
)

func main() {
	cfg := config.DefaultServer()
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	envPath := fs.String("env", ".env", "server env file")
	udpAddr := fs.String("listen", cfg.UDPListen, "UDP listen address")
	stunAlt := fs.String("stun-alt", cfg.StunAltListen, "STUN alternate UDP listen address")
	httpAddr := fs.String("http", cfg.HTTPListen, "HTTP listen address")
	mysqlDSN := fs.String("mysql-dsn", cfg.ControlDatabaseDSN, "MySQL control database DSN")
	psk := fs.String("psk", cfg.PSK, "deployment pre-shared key")
	adminPassword := fs.String("admin-password", cfg.AdminPassword, "initial admin password")
	adminHash := fs.String("admin-password-hash", cfg.AdminPasswordHash, "bcrypt admin password hash")
	adminJWTSecret := fs.String("admin-jwt-secret", cfg.AdminJWTSecret, "admin JWT signing secret")
	adminAccessTokenTTL := fs.Duration("admin-access-token-ttl", cfg.AdminAccessTokenTTL, "admin access token TTL")
	adminRefreshTokenTTL := fs.Duration("admin-refresh-token-ttl", cfg.AdminRefreshTokenTTL, "admin refresh token TTL")
	peerTTL := fs.Duration("peer-ttl", cfg.PeerTTL, "peer TTL")
	pairTTL := fs.Duration("pair-ttl", cfg.PairTTL, "pair TTL")
	relayIdle := fs.Duration("relay-idle-timeout", cfg.RelayIdleTimeout, "relay idle timeout")
	allowRelay := fs.Bool("allow-relay", cfg.AllowRelay, "allow TURN relay forwarding")
	allowLegacy := fs.Bool("allow-legacy", cfg.AllowLegacy, "allow legacy plaintext JSON UDP protocol")
	fs.Parse(os.Args[1:])

	if err := config.LoadServerEnv(*envPath, &cfg); err != nil {
		log.Fatal(err)
	}
	if flagSet(fs, "listen") {
		cfg.UDPListen = *udpAddr
	}
	if flagSet(fs, "stun-alt") {
		cfg.StunAltListen = *stunAlt
	}
	if flagSet(fs, "http") {
		cfg.HTTPListen = *httpAddr
	}
	if flagSet(fs, "mysql-dsn") {
		cfg.ControlDatabaseDSN = *mysqlDSN
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
	if flagSet(fs, "admin-jwt-secret") {
		cfg.AdminJWTSecret = *adminJWTSecret
	}
	if flagSet(fs, "admin-access-token-ttl") {
		cfg.AdminAccessTokenTTL = *adminAccessTokenTTL
	}
	if flagSet(fs, "admin-refresh-token-ttl") {
		cfg.AdminRefreshTokenTTL = *adminRefreshTokenTTL
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

	app, err := serverapp.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	if err := app.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
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
