//go:build windows

package lan

import (
	"os/exec"
	"strings"
	"syscall"
)

func machineUUID() string {
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", "(Get-CimInstance Win32_ComputerSystemProduct).UUID")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	uuid := strings.TrimSpace(string(out))
	if uuid == "" || strings.EqualFold(uuid, "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF") {
		return ""
	}
	return uuid
}
