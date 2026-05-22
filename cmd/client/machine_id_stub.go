//go:build !windows

package main

import (
	"os"
	"strings"
)

func machineUUID() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		b, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(b)) != "" {
			return strings.TrimSpace(string(b))
		}
	}
	host, _ := os.Hostname()
	return host
}
