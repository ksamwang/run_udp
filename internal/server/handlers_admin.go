package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"udp_tunnel_demo/internal/store"
)

func (a *App) handleDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := a.enrichedDevices(r.Context())
	writeJSONOrError(w, devices, err)
}

func (a *App) handleAdminDevice(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/devices/")
	a.handleDeviceByID(w, r, id)
}

func (a *App) handleDeviceByID(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		d, err := a.db.GetDevice(r.Context(), id)
		writeJSONOrError(w, d, err)
	case http.MethodPatch:
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
			return
		}
		err := a.db.SetDeviceEnabled(r.Context(), id, req.Enabled)
		if err == nil {
			_ = a.db.Audit(r.Context(), "device_set_enabled", fmt.Sprintf("%s enabled=%v", id, req.Enabled))
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	case http.MethodDelete:
		n, err := a.db.EnabledRuleReferenceCount(r.Context(), id)
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		if n > 0 {
			writeJSONOrError(w, nil, badRequest("device_in_use", "device is still referenced by enabled rules"))
			return
		}
		err = a.db.DeleteDevice(r.Context(), id)
		if err == nil {
			_ = a.db.Audit(r.Context(), "device_delete", id)
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleForwards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules, err := a.enrichedRules(r.Context())
		writeJSONOrError(w, rules, err)
	case http.MethodPost:
		rule, err := decodeRule(r)
		if err == nil {
			err = normalizeRuleValidationError(rule.Validate(), rule)
		}
		if err == nil {
			err = a.validateRule(r.Context(), rule, 0)
		}
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		rule, err = a.db.CreateRule(r.Context(), rule)
		if err == nil {
			_ = a.db.Audit(r.Context(), "rule_create", fmt.Sprintf("#%d %s->%s:%d", rule.ID, rule.SourceID, rule.TargetID, rule.LocalPort))
		}
		writeJSONOrError(w, rule, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleAdminForward(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/admin/rules/"), 10, 64)
	a.handleForwardByID(w, r, id, err)
}

func (a *App) handleForwardByID(w http.ResponseWriter, r *http.Request, id int64, err error) {
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		rule, err := decodeRule(r)
		if err == nil {
			err = normalizeRuleValidationError(rule.Validate(), rule)
		}
		if err == nil {
			err = a.validateRule(r.Context(), rule, id)
		}
		if err != nil {
			writeJSONOrError(w, nil, err)
			return
		}
		err = a.db.UpdateRule(r.Context(), id, rule)
		if err == nil {
			_ = a.db.Audit(r.Context(), "rule_update", fmt.Sprintf("#%d %s->%s:%d", id, rule.SourceID, rule.TargetID, rule.LocalPort))
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	case http.MethodDelete:
		err := a.db.DeleteRule(r.Context(), id)
		if err == nil {
			_ = a.db.Audit(r.Context(), "rule_delete", fmt.Sprintf("#%d", id))
		}
		writeJSONOrError(w, map[string]any{"ok": true}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := a.db.ListSessions(r.Context())
	writeJSONOrError(w, sessions, err)
}

func (a *App) handleTunnelStates(w http.ResponseWriter, r *http.Request) {
	states, err := a.db.ListTunnelStates(r.Context())
	writeJSONOrError(w, states, err)
}

func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := a.db.Metrics(r.Context())
	writeJSONOrError(w, metrics, err)
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.cfgMu.RLock()
		resp := map[string]any{
			"udp_listen":                               a.cfg.UDPListen,
			"stun_alt_listen":                          a.cfg.StunAltListen,
			"http_listen":                              a.cfg.HTTPListen,
			"control_database_configured":              a.cfg.ControlDatabaseDSN != "",
			"psk_configured":                           a.cfg.PSK != "",
			"peer_ttl":                                 a.cfg.PeerTTL.String(),
			"pair_ttl":                                 a.cfg.PairTTL.String(),
			"relay_idle_timeout":                       a.cfg.RelayIdleTimeout.String(),
			"allow_relay":                              a.cfg.AllowRelay,
			"allow_legacy":                             a.cfg.AllowLegacy,
			"client_no_upnp":                           a.cfg.ClientNoUPnP,
			"client_upnp_timeout":                      a.cfg.ClientUPnPTimeout.String(),
			"client_log_level":                         a.cfg.ClientLogLevel,
			"client_tray_enabled":                      a.cfg.ClientTrayEnabled,
			"client_punch_timeout":                     a.cfg.ClientPunchTimeout.String(),
			"client_force_relay":                       a.cfg.ClientForceRelay,
			"client_allow_legacy":                      a.cfg.ClientAllowLegacy,
			"client_release_version":                   a.cfg.ClientReleaseVersion,
			"client_release_url":                       a.cfg.ClientReleaseURL,
			"client_release_sha256":                    a.cfg.ClientReleaseSHA256,
			"client_release_published_at":              a.cfg.ClientReleasePublishedAt,
			"client_release_notes":                     a.cfg.ClientReleaseNotes,
			"client_release_minimum_supported_version": a.cfg.ClientReleaseMinimumSupported,
			"client_release_file":                      a.cfg.ClientReleaseFile,
			"restart_only_fields":                      []string{"udp_listen", "stun_alt_listen", "http_listen", "control_database_dsn", "psk"},
		}
		a.cfgMu.RUnlock()
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPatch:
		var req struct {
			PeerTTL                       string `json:"peer_ttl"`
			PairTTL                       string `json:"pair_ttl"`
			RelayIdleTimeout              string `json:"relay_idle_timeout"`
			AllowRelay                    bool   `json:"allow_relay"`
			AllowLegacy                   bool   `json:"allow_legacy"`
			ClientNoUPnP                  bool   `json:"client_no_upnp"`
			ClientUPnPTimeout             string `json:"client_upnp_timeout"`
			ClientLogLevel                string `json:"client_log_level"`
			ClientTrayEnabled             bool   `json:"client_tray_enabled"`
			ClientPunchTimeout            string `json:"client_punch_timeout"`
			ClientForceRelay              bool   `json:"client_force_relay"`
			ClientAllowLegacy             bool   `json:"client_allow_legacy"`
			ClientReleaseVersion          string `json:"client_release_version"`
			ClientReleaseURL              string `json:"client_release_url"`
			ClientReleaseSHA256           string `json:"client_release_sha256"`
			ClientReleasePublishedAt      string `json:"client_release_published_at"`
			ClientReleaseNotes            string `json:"client_release_notes"`
			ClientReleaseMinimumSupported string `json:"client_release_minimum_supported_version"`
			ClientReleaseFile             string `json:"client_release_file"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		peerTTL, err := time.ParseDuration(req.PeerTTL)
		if err != nil {
			http.Error(w, "bad peer_ttl", http.StatusBadRequest)
			return
		}
		pairTTL, err := time.ParseDuration(req.PairTTL)
		if err != nil {
			http.Error(w, "bad pair_ttl", http.StatusBadRequest)
			return
		}
		relayIdle, err := time.ParseDuration(req.RelayIdleTimeout)
		if err != nil {
			http.Error(w, "bad relay_idle_timeout", http.StatusBadRequest)
			return
		}
		if peerTTL < 10*time.Second || pairTTL < 10*time.Second || relayIdle < 10*time.Second {
			http.Error(w, "durations must be at least 10s", http.StatusBadRequest)
			return
		}
		clientUPnPTimeout, err := time.ParseDuration(req.ClientUPnPTimeout)
		if err != nil {
			http.Error(w, "bad client_upnp_timeout", http.StatusBadRequest)
			return
		}
		clientPunchTimeout, err := time.ParseDuration(req.ClientPunchTimeout)
		if err != nil {
			http.Error(w, "bad client_punch_timeout", http.StatusBadRequest)
			return
		}
		if clientUPnPTimeout < time.Second || clientPunchTimeout < time.Second {
			http.Error(w, "client durations must be at least 1s", http.StatusBadRequest)
			return
		}
		a.cfgMu.Lock()
		a.cfg.PeerTTL = peerTTL
		a.cfg.PairTTL = pairTTL
		a.cfg.RelayIdleTimeout = relayIdle
		a.cfg.AllowRelay = req.AllowRelay
		a.cfg.AllowLegacy = req.AllowLegacy
		a.cfg.ClientNoUPnP = req.ClientNoUPnP
		a.cfg.ClientUPnPTimeout = clientUPnPTimeout
		a.cfg.ClientLogLevel = req.ClientLogLevel
		a.cfg.ClientTrayEnabled = req.ClientTrayEnabled
		a.cfg.ClientPunchTimeout = clientPunchTimeout
		a.cfg.ClientForceRelay = req.ClientForceRelay
		a.cfg.ClientAllowLegacy = req.ClientAllowLegacy
		a.cfg.ClientReleaseVersion = req.ClientReleaseVersion
		a.cfg.ClientReleaseURL = req.ClientReleaseURL
		a.cfg.ClientReleaseSHA256 = req.ClientReleaseSHA256
		a.cfg.ClientReleasePublishedAt = req.ClientReleasePublishedAt
		a.cfg.ClientReleaseNotes = req.ClientReleaseNotes
		a.cfg.ClientReleaseMinimumSupported = req.ClientReleaseMinimumSupported
		a.cfg.ClientReleaseFile = req.ClientReleaseFile
		a.cfgMu.Unlock()
		settings := map[string]string{
			settingPeerTTL:                       peerTTL.String(),
			settingPairTTL:                       pairTTL.String(),
			settingRelayIdleTimeout:              relayIdle.String(),
			settingAllowRelay:                    strconv.FormatBool(req.AllowRelay),
			settingAllowLegacy:                   strconv.FormatBool(req.AllowLegacy),
			settingClientNoUPnP:                  strconv.FormatBool(req.ClientNoUPnP),
			settingClientUPnPTimeout:             clientUPnPTimeout.String(),
			settingClientLogLevel:                req.ClientLogLevel,
			settingClientTrayEnabled:             strconv.FormatBool(req.ClientTrayEnabled),
			settingClientPunchTimeout:            clientPunchTimeout.String(),
			settingClientForceRelay:              strconv.FormatBool(req.ClientForceRelay),
			settingClientAllowLegacy:             strconv.FormatBool(req.ClientAllowLegacy),
			settingClientReleaseVersion:          req.ClientReleaseVersion,
			settingClientReleaseURL:              req.ClientReleaseURL,
			settingClientReleaseSHA256:           req.ClientReleaseSHA256,
			settingClientReleasePublishedAt:      req.ClientReleasePublishedAt,
			settingClientReleaseNotes:            req.ClientReleaseNotes,
			settingClientReleaseMinimumSupported: req.ClientReleaseMinimumSupported,
			settingClientReleaseFile:             req.ClientReleaseFile,
		}
		for key, value := range settings {
			if err := a.db.PutSystemSetting(r.Context(), key, value); err != nil {
				writeJSONOrError(w, nil, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(req.NewPassword) < 8 {
		http.Error(w, "new password must be at least 8 chars", http.StatusBadRequest)
		return
	}
	hash, _ := a.db.GetMeta(r.Context(), "admin_password_hash")
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword)) != nil {
		http.Error(w, "current password is wrong", http.StatusUnauthorized)
		return
	}
	next, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.db.PutMeta(r.Context(), "admin_password_hash", string(next)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func decodeRule(r *http.Request) (store.ForwardRule, error) {
	var rule store.ForwardRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		return rule, err
	}
	rule.Profile = store.NormalizeProfile(rule.Profile)
	return rule, nil
}

func normalizeRuleValidationError(err error, rule store.ForwardRule) error {
	if err == nil {
		return nil
	}
	if rule.SourceID == rule.TargetID && strings.TrimSpace(rule.SourceID) != "" {
		return badRequest("same_device_forbidden", "source_id and target_id must differ")
	}
	return badRequest("bad_rule", err.Error())
}

func (a *App) validateRule(ctx context.Context, rule store.ForwardRule, excludeID int64) error {
	if strings.TrimSpace(rule.SourceID) == "" || strings.TrimSpace(rule.TargetID) == "" {
		return badRequest("device_not_found", "source_id and target_id are required")
	}
	source, err := a.db.GetDevice(ctx, rule.SourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return badRequest("device_not_found", "source device not found")
		}
		return err
	}
	target, err := a.db.GetDevice(ctx, rule.TargetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return badRequest("device_not_found", "target device not found")
		}
		return err
	}
	if source.ID == target.ID {
		return badRequest("same_device_forbidden", "source_id and target_id must differ")
	}
	if !source.Enabled || !target.Enabled {
		return badRequest("device_disabled", "source or target device is disabled")
	}
	if !rule.Enabled {
		return nil
	}
	conflict, err := a.db.LocalPortConflict(ctx, rule.SourceID, rule.LocalPort, excludeID)
	if err != nil {
		return err
	}
	if conflict {
		return badRequest("local_port_conflict", "same source_id cannot reuse local_port across enabled rules")
	}
	return nil
}
