package server

import (
	"context"
	"strconv"
	"time"
)

const (
	settingPeerTTL                       = "peer_ttl"
	settingPairTTL                       = "pair_ttl"
	settingRelayIdleTimeout              = "relay_idle_timeout"
	settingAllowRelay                    = "allow_relay"
	settingLANAllowRelay                 = "lan_allow_relay"
	settingAllowLegacy                   = "allow_legacy"
	settingClientNoUPnP                  = "client_no_upnp"
	settingClientUPnPTimeout             = "client_upnp_timeout"
	settingClientLogLevel                = "client_log_level"
	settingClientTrayEnabled             = "client_tray_enabled"
	settingClientPunchTimeout            = "client_punch_timeout"
	settingClientForceRelay              = "client_force_relay"
	settingClientAllowLegacy             = "client_allow_legacy"
	settingClientReleaseVersion          = "client_release_version"
	settingClientReleaseURL              = "client_release_url"
	settingClientReleaseSHA256           = "client_release_sha256"
	settingClientReleasePublishedAt      = "client_release_published_at"
	settingClientReleaseNotes            = "client_release_notes"
	settingClientReleaseMinimumSupported = "client_release_minimum_supported_version"
	settingClientReleaseFile             = "client_release_file"
	settingLANReleaseVersion             = "lan_release_version"
	settingLANReleaseURL                 = "lan_release_url"
	settingLANReleaseSHA256              = "lan_release_sha256"
	settingLANReleasePublishedAt         = "lan_release_published_at"
	settingLANReleaseNotes               = "lan_release_notes"
	settingLANReleaseMinimumSupported    = "lan_release_minimum_supported_version"
	settingLANReleaseFile                = "lan_release_file"
)

func (a *App) applyStoredSettings() error {
	ctx := rctx()
	if err := a.ensureDefaultSettings(ctx); err != nil {
		return err
	}

	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.applySystemSettingsLocked(ctx)
}

func (a *App) ensureDefaultSettings(ctx context.Context) error {
	a.cfgMu.RLock()
	defaults := map[string]string{
		settingPeerTTL:                       a.cfg.PeerTTL.String(),
		settingPairTTL:                       a.cfg.PairTTL.String(),
		settingRelayIdleTimeout:              a.cfg.RelayIdleTimeout.String(),
		settingAllowRelay:                    strconv.FormatBool(a.cfg.AllowRelay),
		settingLANAllowRelay:                 strconv.FormatBool(a.cfg.LANAllowRelay),
		settingAllowLegacy:                   strconv.FormatBool(a.cfg.AllowLegacy),
		settingClientNoUPnP:                  strconv.FormatBool(a.cfg.ClientNoUPnP),
		settingClientUPnPTimeout:             a.cfg.ClientUPnPTimeout.String(),
		settingClientLogLevel:                a.cfg.ClientLogLevel,
		settingClientTrayEnabled:             strconv.FormatBool(a.cfg.ClientTrayEnabled),
		settingClientPunchTimeout:            a.cfg.ClientPunchTimeout.String(),
		settingClientForceRelay:              strconv.FormatBool(a.cfg.ClientForceRelay),
		settingClientAllowLegacy:             strconv.FormatBool(a.cfg.ClientAllowLegacy),
		settingClientReleaseVersion:          a.cfg.ClientReleaseVersion,
		settingClientReleaseURL:              a.cfg.ClientReleaseURL,
		settingClientReleaseSHA256:           a.cfg.ClientReleaseSHA256,
		settingClientReleasePublishedAt:      a.cfg.ClientReleasePublishedAt,
		settingClientReleaseNotes:            a.cfg.ClientReleaseNotes,
		settingClientReleaseMinimumSupported: a.cfg.ClientReleaseMinimumSupported,
		settingClientReleaseFile:             a.cfg.ClientReleaseFile,
		settingLANReleaseVersion:             a.cfg.LANReleaseVersion,
		settingLANReleaseURL:                 a.cfg.LANReleaseURL,
		settingLANReleaseSHA256:              a.cfg.LANReleaseSHA256,
		settingLANReleasePublishedAt:         a.cfg.LANReleasePublishedAt,
		settingLANReleaseNotes:               a.cfg.LANReleaseNotes,
		settingLANReleaseMinimumSupported:    a.cfg.LANReleaseMinimumSupported,
		settingLANReleaseFile:                a.cfg.LANReleaseFile,
	}
	a.cfgMu.RUnlock()

	for key, value := range defaults {
		existing, err := a.db.GetSystemSetting(ctx, key)
		if err != nil {
			return err
		}
		if existing == "" {
			legacy, err := a.db.GetMeta(ctx, "setting_"+key)
			if err != nil {
				return err
			}
			if legacy != "" {
				value = legacy
			}
			if err := a.db.PutSystemSetting(ctx, key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) applySystemSettingsLocked(ctx context.Context) error {
	for _, item := range []struct {
		key string
		dst *time.Duration
	}{
		{settingPeerTTL, &a.cfg.PeerTTL},
		{settingPairTTL, &a.cfg.PairTTL},
		{settingRelayIdleTimeout, &a.cfg.RelayIdleTimeout},
		{settingClientUPnPTimeout, &a.cfg.ClientUPnPTimeout},
		{settingClientPunchTimeout, &a.cfg.ClientPunchTimeout},
	} {
		value, err := a.db.GetSystemSetting(ctx, item.key)
		if err != nil {
			return err
		}
		if value == "" {
			continue
		}
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*item.dst = parsed
	}
	for _, item := range []struct {
		key string
		dst *bool
	}{
		{settingAllowRelay, &a.cfg.AllowRelay},
		{settingLANAllowRelay, &a.cfg.LANAllowRelay},
		{settingAllowLegacy, &a.cfg.AllowLegacy},
		{settingClientNoUPnP, &a.cfg.ClientNoUPnP},
		{settingClientTrayEnabled, &a.cfg.ClientTrayEnabled},
		{settingClientForceRelay, &a.cfg.ClientForceRelay},
		{settingClientAllowLegacy, &a.cfg.ClientAllowLegacy},
	} {
		value, err := a.db.GetSystemSetting(ctx, item.key)
		if err != nil {
			return err
		}
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		*item.dst = parsed
	}
	for _, item := range []struct {
		key string
		dst *string
	}{
		{settingClientLogLevel, &a.cfg.ClientLogLevel},
		{settingClientReleaseVersion, &a.cfg.ClientReleaseVersion},
		{settingClientReleaseURL, &a.cfg.ClientReleaseURL},
		{settingClientReleaseSHA256, &a.cfg.ClientReleaseSHA256},
		{settingClientReleasePublishedAt, &a.cfg.ClientReleasePublishedAt},
		{settingClientReleaseNotes, &a.cfg.ClientReleaseNotes},
		{settingClientReleaseMinimumSupported, &a.cfg.ClientReleaseMinimumSupported},
		{settingClientReleaseFile, &a.cfg.ClientReleaseFile},
		{settingLANReleaseVersion, &a.cfg.LANReleaseVersion},
		{settingLANReleaseURL, &a.cfg.LANReleaseURL},
		{settingLANReleaseSHA256, &a.cfg.LANReleaseSHA256},
		{settingLANReleasePublishedAt, &a.cfg.LANReleasePublishedAt},
		{settingLANReleaseNotes, &a.cfg.LANReleaseNotes},
		{settingLANReleaseMinimumSupported, &a.cfg.LANReleaseMinimumSupported},
		{settingLANReleaseFile, &a.cfg.LANReleaseFile},
	} {
		value, err := a.db.GetSystemSetting(ctx, item.key)
		if err != nil {
			return err
		}
		*item.dst = value
	}
	return nil
}

func (a *App) currentPeerTTL() time.Duration {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.PeerTTL
}

func (a *App) currentPairTTL() time.Duration {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.PairTTL
}

func (a *App) currentRelayIdleTimeout() time.Duration {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.RelayIdleTimeout
}

func (a *App) currentAllowRelay() bool {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.AllowRelay
}

func (a *App) currentLANAllowRelay() bool {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.LANAllowRelay
}

func (a *App) currentAllowLegacy() bool {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.AllowLegacy
}
