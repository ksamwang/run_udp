//go:build !windows

package main

import (
	"udp_tunnel_demo/internal/config"
)

func saveClientConfigWithElevation(configPath string, cfg config.Client) (bool, error) {
	return false, config.SaveClientLocalJSON(configPath, cfg)
}

func restartWindowsServiceWithElevation() error {
	return restartWindowsService()
}
