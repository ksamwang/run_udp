//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"udp_tunnel_demo/internal/lan"
)

func saveLANConfigWithElevation(configPath string, cfg lan.Config) (bool, error) {
	if err := lan.SaveConfig(configPath, cfg); err == nil {
		return false, nil
	} else if !isPermissionDenied(err) {
		return false, err
	}

	tmp, err := os.CreateTemp("", "udp-tunnel-lan-*.json")
	if err != nil {
		return false, fmt.Errorf("create elevated LAN config temp: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return false, err
	}
	if err := lan.SaveConfig(tmpPath, cfg); err != nil {
		_ = os.Remove(tmpPath)
		return false, err
	}
	if err := runElevatedSelf("-apply-lan-config", tmpPath, "-config", configPath); err != nil {
		_ = os.Remove(tmpPath)
		return false, err
	}
	return true, nil
}

func restartWindowsServiceWithElevation() error {
	if err := restartWindowsService(); err == nil {
		return nil
	} else if !isPermissionDenied(err) {
		return err
	}
	return runElevatedSelf("-restart-service")
}

func runElevatedSelf(args ...string) error {
	exe := currentExePath()
	if exe == "" {
		return errors.New("executable path unavailable")
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(joinWindowsArgs(args))
	cwd, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	return windows.ShellExecute(0, verb, file, params, cwd, windows.SW_SHOWNORMAL)
}

func joinWindowsArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, quoteWindowsArg(arg))
	}
	return strings.Join(quoted, " ")
}

func quoteWindowsArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\n\v\"") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range arg {
		switch r {
		case '\\':
			backslashes++
		case '"':
			b.WriteString(strings.Repeat("\\", backslashes*2+1))
			b.WriteRune(r)
			backslashes = 0
		default:
			if backslashes > 0 {
				b.WriteString(strings.Repeat("\\", backslashes))
				backslashes = 0
			}
			b.WriteRune(r)
		}
	}
	if backslashes > 0 {
		b.WriteString(strings.Repeat("\\", backslashes*2))
	}
	b.WriteByte('"')
	return b.String()
}

func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "拒绝访问")
}
