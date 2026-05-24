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
		writeJSONOrError(w, nil, methodNotAllowed())
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
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleAdminForward(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/admin/rules/"), 10, 64)
	a.handleForwardByID(w, r, id, err)
}

func (a *App) handleForwardByID(w http.ResponseWriter, r *http.Request, id int64, err error) {
	if err != nil {
		writeJSONOrError(w, nil, badRequest("bad_id", "bad id"))
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
		writeJSONOrError(w, nil, methodNotAllowed())
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

func (a *App) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONOrError(w, nil, methodNotAllowed())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	filter := store.AuditFilter{
		Kind:    strings.TrimSpace(r.URL.Query().Get("kind")),
		Keyword: strings.TrimSpace(r.URL.Query().Get("keyword")),
		From:    strings.TrimSpace(r.URL.Query().Get("from")),
		To:      strings.TrimSpace(r.URL.Query().Get("to")),
		Limit:   limit,
	}
	events, err := a.db.ListAuditEvents(r.Context(), filter)
	writeJSONOrError(w, events, err)
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
			"lan_allow_relay":                          a.cfg.LANAllowRelay,
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
			"lan_release_version":                      a.cfg.LANReleaseVersion,
			"lan_release_url":                          a.cfg.LANReleaseURL,
			"lan_release_sha256":                       a.cfg.LANReleaseSHA256,
			"lan_release_published_at":                 a.cfg.LANReleasePublishedAt,
			"lan_release_notes":                        a.cfg.LANReleaseNotes,
			"lan_release_minimum_supported_version":    a.cfg.LANReleaseMinimumSupported,
			"lan_release_file":                         a.cfg.LANReleaseFile,
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
			LANAllowRelay                 bool   `json:"lan_allow_relay"`
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
			LANReleaseVersion             string `json:"lan_release_version"`
			LANReleaseURL                 string `json:"lan_release_url"`
			LANReleaseSHA256              string `json:"lan_release_sha256"`
			LANReleasePublishedAt         string `json:"lan_release_published_at"`
			LANReleaseNotes               string `json:"lan_release_notes"`
			LANReleaseMinimumSupported    string `json:"lan_release_minimum_supported_version"`
			LANReleaseFile                string `json:"lan_release_file"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
			return
		}
		peerTTL, err := time.ParseDuration(req.PeerTTL)
		if err != nil {
			writeJSONOrError(w, nil, badRequest("bad_peer_ttl", "bad peer_ttl"))
			return
		}
		pairTTL, err := time.ParseDuration(req.PairTTL)
		if err != nil {
			writeJSONOrError(w, nil, badRequest("bad_pair_ttl", "bad pair_ttl"))
			return
		}
		relayIdle, err := time.ParseDuration(req.RelayIdleTimeout)
		if err != nil {
			writeJSONOrError(w, nil, badRequest("bad_relay_idle_timeout", "bad relay_idle_timeout"))
			return
		}
		if peerTTL < 10*time.Second || pairTTL < 10*time.Second || relayIdle < 10*time.Second {
			writeJSONOrError(w, nil, badRequest("duration_too_short", "durations must be at least 10s"))
			return
		}
		clientUPnPTimeout, err := time.ParseDuration(req.ClientUPnPTimeout)
		if err != nil {
			writeJSONOrError(w, nil, badRequest("bad_client_upnp_timeout", "bad client_upnp_timeout"))
			return
		}
		clientPunchTimeout, err := time.ParseDuration(req.ClientPunchTimeout)
		if err != nil {
			writeJSONOrError(w, nil, badRequest("bad_client_punch_timeout", "bad client_punch_timeout"))
			return
		}
		if clientUPnPTimeout < time.Second || clientPunchTimeout < time.Second {
			writeJSONOrError(w, nil, badRequest("client_duration_too_short", "client durations must be at least 1s"))
			return
		}
		a.cfgMu.Lock()
		a.cfg.PeerTTL = peerTTL
		a.cfg.PairTTL = pairTTL
		a.cfg.RelayIdleTimeout = relayIdle
		a.cfg.AllowRelay = req.AllowRelay
		a.cfg.LANAllowRelay = req.LANAllowRelay
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
		a.cfg.LANReleaseVersion = req.LANReleaseVersion
		a.cfg.LANReleaseURL = req.LANReleaseURL
		a.cfg.LANReleaseSHA256 = req.LANReleaseSHA256
		a.cfg.LANReleasePublishedAt = req.LANReleasePublishedAt
		a.cfg.LANReleaseNotes = req.LANReleaseNotes
		a.cfg.LANReleaseMinimumSupported = req.LANReleaseMinimumSupported
		a.cfg.LANReleaseFile = req.LANReleaseFile
		a.cfgMu.Unlock()
		settings := map[string]string{
			settingPeerTTL:                       peerTTL.String(),
			settingPairTTL:                       pairTTL.String(),
			settingRelayIdleTimeout:              relayIdle.String(),
			settingAllowRelay:                    strconv.FormatBool(req.AllowRelay),
			settingLANAllowRelay:                 strconv.FormatBool(req.LANAllowRelay),
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
			settingLANReleaseVersion:             req.LANReleaseVersion,
			settingLANReleaseURL:                 req.LANReleaseURL,
			settingLANReleaseSHA256:              req.LANReleaseSHA256,
			settingLANReleasePublishedAt:         req.LANReleasePublishedAt,
			settingLANReleaseNotes:               req.LANReleaseNotes,
			settingLANReleaseMinimumSupported:    req.LANReleaseMinimumSupported,
			settingLANReleaseFile:                req.LANReleaseFile,
		}
		for key, value := range settings {
			if err := a.db.PutSystemSetting(r.Context(), key, value); err != nil {
				writeJSONOrError(w, nil, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeJSONOrError(w, nil, methodNotAllowed())
	}
}

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONOrError(w, nil, methodNotAllowed())
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
		return
	}
	if len(req.NewPassword) < 8 {
		writeJSONOrError(w, nil, badRequest("password_too_short", "new password must be at least 8 chars"))
		return
	}
	claims, _ := r.Context().Value(adminClaimsKey{}).(adminClaims)
	user, err := a.db.GetAdminUserByID(r.Context(), claims.Subject)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if claims.PasswordVersion != 0 && user.PasswordVersion != claims.PasswordVersion {
		writeJSONOrError(w, nil, unauthorized("unauthorized", "unauthorized"))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
		writeJSONOrError(w, nil, unauthorized("wrong_current_password", "current password is wrong"))
		return
	}
	next, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if err := a.db.UpdateAdminPassword(r.Context(), user.ID, string(next)); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if err := a.db.ClearAdminPasswordChangeRequired(r.Context(), user.ID); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if err := a.db.RevokeAdminRefreshTokensByUser(r.Context(), user.ID); err != nil {
		writeJSONOrError(w, nil, err)
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
