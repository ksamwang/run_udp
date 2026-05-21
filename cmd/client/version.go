package main

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

const (
	windowsServiceName = "UDPTunnelAgent"
	windowsRunKeyName  = "UDPTunnelTray"
)

type clientRuntimeInfo struct {
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	BuildTime       string `json:"build_time"`
	InstallPath     string `json:"install_path"`
	LogPath         string `json:"log_path"`
	ServiceStatus   string `json:"service_status"`
	UpdateStatus    string `json:"update_status"`
	LastUpdateCheck string `json:"last_update_check"`
	LastUpdateError string `json:"last_update_error"`
}

type runtimeState struct {
	mu sync.RWMutex

	logPath         string
	serviceStatus   string
	updateStatus    string
	lastUpdateCheck string
	lastUpdateError string
}

var appRuntime runtimeState

func (r *runtimeState) SetLogPath(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logPath = path
}

func (r *runtimeState) SetServiceStatus(v string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serviceStatus = v
}

func (r *runtimeState) SetUpdateStatus(status, lastCheck, lastErr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if status != "" {
		r.updateStatus = status
	}
	if lastCheck != "" {
		r.lastUpdateCheck = lastCheck
	}
	r.lastUpdateError = lastErr
}

func currentRuntimeInfo() clientRuntimeInfo {
	appRuntime.mu.RLock()
	defer appRuntime.mu.RUnlock()
	installPath, _ := exeDir()
	return clientRuntimeInfo{
		Version:         Version,
		Commit:          Commit,
		BuildTime:       BuildTime,
		InstallPath:     installPath,
		LogPath:         appRuntime.logPath,
		ServiceStatus:   appRuntime.serviceStatus,
		UpdateStatus:    appRuntime.updateStatus,
		LastUpdateCheck: appRuntime.lastUpdateCheck,
		LastUpdateError: appRuntime.lastUpdateError,
	}
}

func currentExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err == nil {
		exe = resolved
	}
	return exe
}
