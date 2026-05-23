//go:build !windows

package main

import "udp_tunnel_demo/internal/lan"

func saveLANConfigWithElevation(configPath string, cfg lan.Config) (bool, error) {
	return false, lan.SaveConfig(configPath, cfg)
}

func restartWindowsServiceWithElevation() error {
	return restartWindowsService()
}
