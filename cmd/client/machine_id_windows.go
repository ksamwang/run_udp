//go:build windows

package main

import (
	"os/exec"
	"strings"
)

func machineUUID() string {
	out, err := exec.Command("powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_ComputerSystemProduct).UUID").Output()
	if err != nil {
		return ""
	}
	uuid := strings.TrimSpace(string(out))
	if uuid == "" || strings.EqualFold(uuid, "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF") {
		return ""
	}
	return uuid
}
