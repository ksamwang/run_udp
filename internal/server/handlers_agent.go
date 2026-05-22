package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"udp_tunnel_demo/internal/store"
)

func (a *App) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string              `json:"device_id"`
		Name     string              `json:"name"`
		Addr     string              `json:"addr"`
		UpnpAddr string              `json:"upnp_addr"`
		NATType  string              `json:"nat_type"`
		Tunnels  []agentTunnelReport `json:"tunnels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
		writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
		return
	}
	if err := a.ensureAgentDeviceAllowed(r.Context(), req.DeviceID); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	addr := req.Addr
	if addr == "" {
		addr = requestAddr(r)
	}
	name := req.Name
	err := a.db.UpsertDevice(r.Context(), req.DeviceID, name, addr, req.UpnpAddr, "", a.agentOnline(req.Tunnels))
	if err == nil {
		err = a.putTunnelReports(r.Context(), req.DeviceID, req.Tunnels)
	}
	writeJSONOrError(w, map[string]any{"ok": true}, err)
}

func (a *App) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string              `json:"device_id"`
		Name     string              `json:"name"`
		Addr     string              `json:"addr"`
		UpnpAddr string              `json:"upnp_addr"`
		NATType  string              `json:"nat_type"`
		Tunnels  []agentTunnelReport `json:"tunnels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
		writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
		return
	}
	if err := a.ensureAgentDeviceAllowed(r.Context(), req.DeviceID); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	addr := req.Addr
	if addr == "" {
		addr = requestAddr(r)
	}
	name := req.Name
	err := a.db.UpsertDevice(r.Context(), req.DeviceID, name, addr, req.UpnpAddr, "", a.agentOnline(req.Tunnels))
	if err == nil {
		err = a.putTunnelReports(r.Context(), req.DeviceID, req.Tunnels)
	}
	writeJSONOrError(w, map[string]any{"ok": true}, err)
}

func (a *App) handleAgentTunnelStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID         string `json:"device_id"`
		Name             string `json:"name"`
		Peer             string `json:"peer"`
		Profile          string `json:"profile"`
		State            string `json:"state"`
		Via              string `json:"via"`
		NATType          string `json:"nat_type"`
		Addr             string `json:"addr"`
		UpnpAddr         string `json:"upnp_addr"`
		PublicAddr       string `json:"public_addr"`
		ConvID           int64  `json:"conv_id"`
		RTTMs            int    `json:"rtt_ms"`
		LastError        string `json:"last_error"`
		Attempt          int    `json:"attempt"`
		NextRetryAt      string `json:"next_retry_at"`
		LastTransitionAt string `json:"last_transition_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" || req.Peer == "" {
		writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
		return
	}
	req.Profile = store.NormalizeProfile(req.Profile)
	if err := a.ensureAgentDeviceAllowed(r.Context(), req.DeviceID); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	addr := req.Addr
	if addr == "" {
		addr = requestAddr(r)
	}
	online := req.State == "connecting" || req.State == "p2p" || req.State == "relay"
	err := a.db.UpsertDevice(r.Context(), req.DeviceID, req.Name, addr, req.UpnpAddr, req.Peer, online)
	if err == nil {
		err = a.db.PutTunnelState(r.Context(), store.TunnelState{
			DeviceID:         req.DeviceID,
			PeerID:           req.Peer,
			Profile:          req.Profile,
			State:            req.State,
			Via:              req.Via,
			NATType:          req.NATType,
			PublicAddr:       req.PublicAddr,
			ConvID:           req.ConvID,
			RTTMs:            req.RTTMs,
			LastError:        req.LastError,
			Attempt:          req.Attempt,
			NextRetryAt:      req.NextRetryAt,
			LastTransitionAt: req.LastTransitionAt,
		})
	}
	if err == nil && (req.State == "p2p" || req.State == "relay") && req.Via != "" {
		err = a.db.UpdateSessionPathForPair(r.Context(), req.DeviceID, req.Peer, req.Profile, req.Via)
	}
	writeJSONOrError(w, map[string]any{"ok": true}, err)
}

func (a *App) handleAgentBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONOrError(w, nil, methodNotAllowed())
		return
	}
	var req struct {
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
		writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
		return
	}
	if err := a.ensureAgentDeviceAllowed(r.Context(), req.DeviceID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeJSONOrError(w, nil, err)
		return
	}
	name := strings.TrimSpace(req.DeviceName)
	resp := a.bootstrapConfig(r, req.DeviceID, name)
	err := a.db.UpsertDevice(r.Context(), req.DeviceID, name, requestAddr(r), "", "", false)
	writeJSONOrError(w, resp, err)
}

func (a *App) handleAgentRules(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if deviceID == "" {
		writeJSONOrError(w, nil, badRequest("device_id_required", "device_id required"))
		return
	}
	if err := a.ensureAgentDeviceAllowed(r.Context(), deviceID); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	rules, err := a.db.RulesForDevice(r.Context(), deviceID)
	writeJSONOrError(w, rules, err)
}

func (a *App) handleClientRelease(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.clientRelease(r))
}

func (a *App) handleClientInstaller(w http.ResponseWriter, r *http.Request) {
	a.cfgMu.RLock()
	file := a.cfg.ClientReleaseFile
	a.cfgMu.RUnlock()
	if strings.TrimSpace(file) == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, file)
}

func (a *App) agentOnline(tunnels []agentTunnelReport) bool {
	return true
}

func (a *App) putTunnelReports(ctx context.Context, deviceID string, tunnels []agentTunnelReport) error {
	for _, t := range tunnels {
		if t.Peer == "" {
			continue
		}
		if err := a.db.PutTunnelState(ctx, store.TunnelState{
			DeviceID:         deviceID,
			PeerID:           t.Peer,
			Profile:          t.Profile,
			State:            t.State,
			Via:              t.Via,
			NATType:          t.NATType,
			PublicAddr:       t.PublicAddr,
			ConvID:           t.ConvID,
			RTTMs:            t.RTTMs,
			LastError:        t.LastError,
			Attempt:          t.Attempt,
			NextRetryAt:      t.NextRetryAt,
			LastTransitionAt: t.LastTransitionAt,
		}); err != nil {
			return err
		}
		if (t.State == "p2p" || t.State == "relay") && t.Via != "" {
			if err := a.db.UpdateSessionPathForPair(ctx, deviceID, t.Peer, t.Profile, t.Via); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) ensureAgentDeviceAllowed(ctx context.Context, deviceID string) error {
	d, err := a.db.GetDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	if !d.Enabled {
		return badRequest("device_disabled", "device is disabled")
	}
	return nil
}

func (a *App) bootstrapConfig(r *http.Request, deviceID, deviceName string) agentBootstrapResponse {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return agentBootstrapResponse{
		DeviceID:     deviceID,
		DeviceName:   deviceName,
		Server:       externalUDPAddr(r, a.cfg.UDPListen),
		ServerHTTP:   requestBaseURL(r),
		PSK:          a.cfg.PSK,
		STUNAltPort:  portFromAddr(a.cfg.StunAltListen, 7002),
		NoUPnP:       a.cfg.ClientNoUPnP,
		UPnPTimeout:  a.cfg.ClientUPnPTimeout.String(),
		LogLevel:     a.cfg.ClientLogLevel,
		TrayEnabled:  a.cfg.ClientTrayEnabled,
		PunchTimeout: a.cfg.ClientPunchTimeout.String(),
		ForceRelay:   a.cfg.ClientForceRelay,
		AllowLegacy:  a.cfg.ClientAllowLegacy,
	}
}

func (a *App) clientRelease(r *http.Request) clientReleaseResponse {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	url := strings.TrimSpace(a.cfg.ClientReleaseURL)
	if url == "" && strings.TrimSpace(a.cfg.ClientReleaseFile) != "" {
		url = requestBaseURL(r) + "/downloads/client/installer"
	}
	return clientReleaseResponse{
		Version:                 a.cfg.ClientReleaseVersion,
		URL:                     url,
		SHA256:                  a.cfg.ClientReleaseSHA256,
		PublishedAt:             a.cfg.ClientReleasePublishedAt,
		Notes:                   a.cfg.ClientReleaseNotes,
		MinimumSupportedVersion: a.cfg.ClientReleaseMinimumSupported,
	}
}
