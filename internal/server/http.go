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
	admin.Any("/audit-events", ginWrap(a.requireAdmin(a.handleAuditEvents)))
	admin.Any("/releases/:product/upload", ginWrap(a.requireAdmin(a.handleAdminReleaseUpload)))
	admin.Any("/releases/validate-url", ginWrap(a.requireAdmin(a.handleAdminReleaseValidateURL)))
	admin.Any("/settings", ginWrap(a.requireAdmin(a.handleSettings)))
	admin.POST("/password", ginWrap(a.requireAdmin(a.handleChangePassword)))
	admin.Any("/lan/networks", ginWrap(a.requireAdmin(a.handleAdminLANNetworks)))
	admin.Any("/lan/networks/:id", ginWrap(a.requireAdmin(a.handleAdminLANNetwork)))
	admin.Any("/lan/addresses", ginWrap(a.requireAdmin(a.handleAdminLANAddresses)))
	admin.Any("/lan/addresses/:device_id/:action", ginWrap(a.requireAdmin(a.handleAdminLANAddressAction)))
	admin.Any("/lan/addresses/:device_id", ginWrap(a.requireAdmin(a.handleAdminLANAddress)))
	admin.Any("/lan/device-keys", ginWrap(a.requireAdmin(a.handleAdminLANDeviceKeys)))
	admin.Any("/lan/groups", ginWrap(a.requireAdmin(a.handleAdminLANGroups)))
	admin.Any("/lan/groups/:id", ginWrap(a.requireAdmin(a.handleAdminLANGroup)))
	admin.Any("/lan/acl", ginWrap(a.requireAdmin(a.handleAdminLANACLRules)))
	admin.Any("/lan/acl/:id", ginWrap(a.requireAdmin(a.handleAdminLANACLRule)))
	admin.Any("/lan/routes", ginWrap(a.requireAdmin(a.handleAdminLANRoutes)))
	admin.Any("/lan/routes/:id", ginWrap(a.requireAdmin(a.handleAdminLANRoute)))
	admin.Any("/lan/device-states", ginWrap(a.requireAdmin(a.handleAdminLANDeviceStates)))
	admin.Any("/lan/peer-states", ginWrap(a.requireAdmin(a.handleAdminLANPeerStates)))

	agent := r.Group("/api/agent")
	agent.POST("/bootstrap", ginWrap(a.handleAgentBootstrap))
	agent.POST("/register", ginWrap(a.requireAgent(a.handleAgentRegister)))
	agent.POST("/heartbeat", ginWrap(a.requireAgent(a.handleAgentHeartbeat)))
	agent.POST("/tunnel-status", ginWrap(a.requireAgent(a.handleAgentTunnelStatus)))
	agent.GET("/rules", ginWrap(a.requireAgent(a.handleAgentRules)))

	lan := r.Group("/api/lan")
	lan.POST("/bootstrap", ginWrap(a.handleLANBootstrap))
	lan.POST("/status", ginWrap(a.handleLANStatus))
	lan.POST("/packets/send", ginWrap(a.handleLANPacketSend))
	lan.POST("/packets/poll", ginWrap(a.handleLANPacketPoll))
	lan.GET("/release", ginWrap(a.handleLANRelease))

	r.GET("/api/client/release", ginWrap(a.requireAgent(a.handleClientRelease)))
	r.GET("/downloads/client/installer", ginWrap(a.handleClientInstaller))
	r.GET("/downloads/lan/installer", ginWrap(a.handleLANInstaller))
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
