package server

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (a *App) runHTTP() {
	mux := a.httpMux()
	log.Printf("rendezvous server (HTTP) listening on %s", a.cfg.HTTPListen)
	if err := http.ListenAndServe(a.cfg.HTTPListen, mux); err != nil {
		log.Fatalf("http listen failed: %v", err)
	}
}

func (a *App) httpMux() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), a.ginCORS())

	r.Any("/health", ginWrap(a.handleHealth))
	r.Any("/peers", ginWrap(a.handlePeers))

	admin := r.Group("/api/admin")
	admin.POST("/auth/login", ginWrap(a.handleAdminLogin))
	admin.POST("/auth/refresh", ginWrap(a.handleAdminRefresh))
	admin.POST("/auth/logout", ginWrap(a.handleAdminLogout))
	admin.Any("/me", ginWrap(a.requireAdmin(a.handleAdminMe)))
	admin.Any("/devices", ginWrap(a.requireAdmin(a.handleDevices)))
	admin.Any("/devices/:id", ginWrap(a.requireAdmin(a.handleAdminDevice)))
	admin.Any("/rules", ginWrap(a.requireAdmin(a.handleForwards)))
	admin.Any("/rules/:id", ginWrap(a.requireAdmin(a.handleAdminForward)))
	admin.Any("/sessions", ginWrap(a.requireAdmin(a.handleSessions)))
	admin.Any("/tunnel-states", ginWrap(a.requireAdmin(a.handleTunnelStates)))
	admin.Any("/metrics", ginWrap(a.requireAdmin(a.handleMetrics)))
	admin.Any("/settings", ginWrap(a.requireAdmin(a.handleSettings)))
	admin.POST("/password", ginWrap(a.requireAdmin(a.handleChangePassword)))

	agent := r.Group("/api/agent")
	agent.POST("/bootstrap", ginWrap(a.handleAgentBootstrap))
	agent.POST("/register", ginWrap(a.requireAgent(a.handleAgentRegister)))
	agent.POST("/heartbeat", ginWrap(a.requireAgent(a.handleAgentHeartbeat)))
	agent.POST("/tunnel-status", ginWrap(a.requireAgent(a.handleAgentTunnelStatus)))
	agent.GET("/rules", ginWrap(a.requireAgent(a.handleAgentRules)))

	r.GET("/api/client/release", ginWrap(a.requireAgent(a.handleClientRelease)))
	r.GET("/downloads/client/installer", ginWrap(a.handleClientInstaller))
	return r
}

func ginWrap(h http.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		h(c.Writer, c.Request)
	}
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
