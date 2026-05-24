//go:build windows

package lan

import "udp_tunnel_demo/internal/machineid"

func machineUUID() string {
	return machineid.UUID()
}
