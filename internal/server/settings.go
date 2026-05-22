package server

import (
	"strconv"
	"time"
)

func (a *App) applyStoredSettings() error {
	ctx := rctx()
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	if v, err := a.db.GetMeta(ctx, "setting_peer_ttl"); err != nil {
		return err
	} else if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		a.cfg.PeerTTL = d
	}
	if v, err := a.db.GetMeta(ctx, "setting_pair_ttl"); err != nil {
		return err
	} else if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		a.cfg.PairTTL = d
	}
	if v, err := a.db.GetMeta(ctx, "setting_relay_idle_timeout"); err != nil {
		return err
	} else if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		a.cfg.RelayIdleTimeout = d
	}
	if v, err := a.db.GetMeta(ctx, "setting_allow_relay"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.AllowRelay = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_allow_legacy"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.AllowLegacy = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_no_upnp"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.ClientNoUPnP = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_upnp_timeout"); err != nil {
		return err
	} else if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		a.cfg.ClientUPnPTimeout = d
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_log_level"); err != nil {
		return err
	} else if v != "" {
		a.cfg.ClientLogLevel = v
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_tray_enabled"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.ClientTrayEnabled = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_punch_timeout"); err != nil {
		return err
	} else if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		a.cfg.ClientPunchTimeout = d
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_force_relay"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.ClientForceRelay = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_allow_legacy"); err != nil {
		return err
	} else if v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		a.cfg.ClientAllowLegacy = b
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_release_version"); err != nil {
		return err
	} else if v != "" {
		a.cfg.ClientReleaseVersion = v
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_release_url"); err != nil {
		return err
	} else if v != "" {
		a.cfg.ClientReleaseURL = v
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_release_sha256"); err != nil {
		return err
	} else if v != "" {
		a.cfg.ClientReleaseSHA256 = v
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_release_published_at"); err != nil {
		return err
	} else if v != "" {
		a.cfg.ClientReleasePublishedAt = v
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_release_notes"); err != nil {
		return err
	} else if v != "" {
		a.cfg.ClientReleaseNotes = v
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_release_minimum_supported_version"); err != nil {
		return err
	} else if v != "" {
		a.cfg.ClientReleaseMinimumSupported = v
	}
	if v, err := a.db.GetMeta(ctx, "setting_client_release_file"); err != nil {
		return err
	} else if v != "" {
		a.cfg.ClientReleaseFile = v
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

func (a *App) currentAllowLegacy() bool {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.AllowLegacy
}
