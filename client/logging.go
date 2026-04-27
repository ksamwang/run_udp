package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"udp_tunnel_demo/internal/config"
	"udp_tunnel_demo/internal/store"
)

// setupLogging 把 log 输出切到 exe 同目录下的 client.log。
// 启动时把上一份 rename 成 client.log.1 留作上一次运行的快照。
// 返回最终使用的日志文件路径，便于在启动信息里打印出来。
func setupLogging(deviceID string) string {
	dir, err := exeDir()
	if err != nil {
		dir, _ = os.Getwd()
	}
	logPath := filepath.Join(dir, "client.log")
	prevPath := logPath + ".1"

	if _, err := os.Stat(logPath); err == nil {
		_ = os.Remove(prevPath)
		_ = os.Rename(logPath, prevPath)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// -H=windowsgui exe 没有可用的 stderr，这里就只能放弃日志了
		return ""
	}
	// 不能 MultiWriter 到 os.Stderr：windowsgui 的 stderr 句柄无效，
	// 第一个 writer 返回 error 后 io.MultiWriter 会直接停手，文件就拿不到任何写入。
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	return logPath
}

func logStartup(cfg config.Client, configPath, logPath string, agent, probe bool) {
	mode := "demo"
	switch {
	case probe:
		mode = "probe"
	case agent || (cfg.ServerHTTP != "" && cfg.PeerID == ""):
		mode = "agent"
	}
	log.Printf("[%s] ====== udp-tunnel client startup ======", cfg.DeviceID)
	log.Printf("[%s] mode=%s config=%s log=%s", cfg.DeviceID, mode, configPath, logPath)
	log.Printf("[%s] server=%q server_http=%q peer=%q",
		cfg.DeviceID, cfg.Server, cfg.ServerHTTP, cfg.PeerID)
	log.Printf("[%s] device_name=%q", cfg.DeviceID, cfg.DeviceName)
	log.Printf("[%s] psk_set=%v psk_len=%d allow_legacy=%v force_relay=%v",
		cfg.DeviceID, cfg.PSK != "", len(cfg.PSK), cfg.AllowLegacy, cfg.ForceRelay)
	log.Printf("[%s] no_upnp=%v upnp_timeout=%s punch_timeout=%s tray=%v",
		cfg.DeviceID, cfg.NoUPnP, cfg.UPnPTimeout, cfg.PunchTimeout, cfg.TrayEnabled)
	if len(cfg.Forwards) > 0 {
		log.Printf("[%s] cmdline forwards=%v", cfg.DeviceID, cfg.Forwards)
	}
}

// rulesSignature 返回一份稳定的、可比较的规则摘要，用来检测控制面规则是否变化。
func rulesSignature(rules []store.ForwardRule) string {
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		parts = append(parts, fmt.Sprintf("#%d:%s->%s:%d->%s:%d:en=%v",
			r.ID, r.SourceID, r.TargetID, r.LocalPort, r.TargetHost, r.TargetPort, r.Enabled))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func exeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("os.Executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err == nil {
		exe = resolved
	}
	return filepath.Dir(exe), nil
}
