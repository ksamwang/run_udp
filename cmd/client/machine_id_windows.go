//go:build windows

package main

import "udp_tunnel_demo/internal/machineid"

func machineUUID() string {
	return machineid.UUID()
}
