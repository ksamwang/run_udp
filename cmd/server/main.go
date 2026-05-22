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
	adminJWTSecret := fs.String("admin-jwt-secret", cfg.AdminJWTSecret, "admin JWT signing secret")
	adminAccessTokenTTL := fs.Duration("admin-access-token-ttl", cfg.AdminAccessTokenTTL, "admin access token TTL")
	adminRefreshTokenTTL := fs.Duration("admin-refresh-token-ttl", cfg.AdminRefreshTokenTTL, "admin refresh token TTL")
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
	if flagSet(fs, "admin-jwt-secret") {
		cfg.AdminJWTSecret = *adminJWTSecret
	}
	if flagSet(fs, "admin-access-token-ttl") {
		cfg.AdminAccessTokenTTL = *adminAccessTokenTTL
	}
	if flagSet(fs, "admin-refresh-token-ttl") {
		cfg.AdminRefreshTokenTTL = *adminRefreshTokenTTL
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
