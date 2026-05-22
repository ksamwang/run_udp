package server

import (
	"log"
	"net/http"
	"time"
)

func (a *App) runHTTP() {
	mux := a.httpMux()
	log.Printf("rendezvous server (HTTP) listening on %s", a.cfg.HTTPListen)
	if err := http.ListenAndServe(a.cfg.HTTPListen, mux); err != nil {
		log.Fatalf("http listen failed: %v", err)
	}
}

func (a *App) httpMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/peers", a.handlePeers)
	mux.HandleFunc("/api/admin/auth/login", a.handleAdminLogin)
	mux.HandleFunc("/api/admin/auth/refresh", a.handleAdminRefresh)
	mux.HandleFunc("/api/admin/auth/logout", a.handleAdminLogout)
	mux.HandleFunc("/api/admin/me", a.requireAdmin(a.handleAdminMe))
	mux.HandleFunc("/api/admin/devices", a.requireAdmin(a.handleDevices))
	mux.HandleFunc("/api/admin/devices/", a.requireAdmin(a.handleAdminDevice))
	mux.HandleFunc("/api/admin/rules", a.requireAdmin(a.handleForwards))
	mux.HandleFunc("/api/admin/rules/", a.requireAdmin(a.handleAdminForward))
	mux.HandleFunc("/api/admin/sessions", a.requireAdmin(a.handleSessions))
	mux.HandleFunc("/api/admin/tunnel-states", a.requireAdmin(a.handleTunnelStates))
	mux.HandleFunc("/api/admin/metrics", a.requireAdmin(a.handleMetrics))
	mux.HandleFunc("/api/admin/settings", a.requireAdmin(a.handleSettings))
	mux.HandleFunc("/api/admin/password", a.requireAdmin(a.handleChangePassword))
	mux.HandleFunc("/api/agent/register", a.requireAgent(a.handleAgentRegister))
	mux.HandleFunc("/api/agent/heartbeat", a.requireAgent(a.handleAgentHeartbeat))
	mux.HandleFunc("/api/agent/tunnel-status", a.requireAgent(a.handleAgentTunnelStatus))
	mux.HandleFunc("/api/agent/bootstrap", a.requireAgent(a.handleAgentBootstrap))
	mux.HandleFunc("/api/agent/rules", a.requireAgent(a.handleAgentRules))
	mux.HandleFunc("/api/client/release", a.requireAgent(a.handleClientRelease))
	mux.HandleFunc("/downloads/client/installer", a.handleClientInstaller)
	return a.withCORS(mux)
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	peerCount := 0
	for _, byWant := range a.peers {
		peerCount += len(byWant)
	}
	a.mu.Unlock()
	metrics, _ := a.db.Metrics(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"status":              "ok",
		"uptime_seconds":      int64(time.Since(a.startTime).Seconds()),
		"total_register":      a.totalRegister.Load(),
		"total_paired":        a.totalPaired.Load(),
		"total_relayed_bytes": a.totalRelayed.Load(),
		"current_peers":       peerCount,
		"metrics":             metrics,
		"server_time":         time.Now().Format(time.RFC3339),
	})
}

func (a *App) handlePeers(w http.ResponseWriter, r *http.Request) {
	devices, err := a.db.ListDevices(r.Context())
	writeJSONOrError(w, devices, err)
}
