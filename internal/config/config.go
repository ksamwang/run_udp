package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Server struct {
	UDPListen                     string        `json:"udp_listen"`
	StunAltListen                 string        `json:"stun_alt_listen"`
	HTTPListen                    string        `json:"http_listen"`
	DatabasePath                  string        `json:"database_path"`
	AdminPassword                 string        `json:"admin_password"`
	AdminPasswordHash             string        `json:"admin_password_hash"`
	AdminJWTSecret                string        `json:"admin_jwt_secret"`
	AdminAccessTokenTTL           time.Duration `json:"admin_access_token_ttl"`
	AdminRefreshTokenTTL          time.Duration `json:"admin_refresh_token_ttl"`
	AdminAllowedOrigins           []string      `json:"admin_allowed_origins"`
	PSK                           string        `json:"psk"`
	PeerTTL                       time.Duration `json:"peer_ttl"`
	PairTTL                       time.Duration `json:"pair_ttl"`
	RelayIdleTimeout              time.Duration `json:"relay_idle_timeout"`
	AllowRelay                    bool          `json:"allow_relay"`
	AllowLegacy                   bool          `json:"allow_legacy"`
	ClientNoUPnP                  bool          `json:"client_no_upnp"`
	ClientUPnPTimeout             time.Duration `json:"client_upnp_timeout"`
	ClientLogLevel                string        `json:"client_log_level"`
	ClientTrayEnabled             bool          `json:"client_tray_enabled"`
	ClientPunchTimeout            time.Duration `json:"client_punch_timeout"`
	ClientForceRelay              bool          `json:"client_force_relay"`
	ClientAllowLegacy             bool          `json:"client_allow_legacy"`
	ClientReleaseVersion          string        `json:"client_release_version"`
	ClientReleaseURL              string        `json:"client_release_url"`
	ClientReleaseSHA256           string        `json:"client_release_sha256"`
	ClientReleasePublishedAt      string        `json:"client_release_published_at"`
	ClientReleaseNotes            string        `json:"client_release_notes"`
	ClientReleaseMinimumSupported string        `json:"client_release_minimum_supported_version"`
	ClientReleaseFile             string        `json:"client_release_file"`
}

type Client struct {
	Server       string        `json:"server"`
	ServerHTTP   string        `json:"server_http"`
	DeviceID     string        `json:"device_id"`
	DeviceName   string        `json:"device_name"`
	PeerID       string        `json:"peer_id"`
	PSK          string        `json:"psk"`
	NoUPnP       bool          `json:"no_upnp"`
	UPnPTimeout  time.Duration `json:"upnp_timeout"`
	LogLevel     string        `json:"log_level"`
	TrayEnabled  bool          `json:"tray_enabled"`
	PunchTimeout time.Duration `json:"punch_timeout"`
	ForceRelay   bool          `json:"force_relay"`
	AllowLegacy  bool          `json:"allow_legacy"`
	Forwards     []string      `json:"forwards"`
}

func DefaultServer() Server {
	c := DefaultClient()
	return Server{
		UDPListen:            ":7000",
		StunAltListen:        ":7002",
		HTTPListen:           ":7001",
		DatabasePath:         "udp-tunnel.db",
		AdminAccessTokenTTL:  time.Hour,
		AdminRefreshTokenTTL: 30 * 24 * time.Hour,
		PeerTTL:              90 * time.Second,
		PairTTL:              2 * time.Minute,
		RelayIdleTimeout:     5 * time.Minute,
		AllowRelay:           true,
		AllowLegacy:          false,
		ClientNoUPnP:         c.NoUPnP,
		ClientUPnPTimeout:    c.UPnPTimeout,
		ClientLogLevel:       c.LogLevel,
		ClientTrayEnabled:    c.TrayEnabled,
		ClientPunchTimeout:   c.PunchTimeout,
		ClientForceRelay:     c.ForceRelay,
		ClientAllowLegacy:    c.AllowLegacy,
	}
}

func DefaultClient() Client {
	return Client{
		UPnPTimeout:  4 * time.Second,
		LogLevel:     "info",
		TrayEnabled:  true,
		PunchTimeout: 30 * time.Second,
		AllowLegacy:  false,
	}
}

func (s *Server) UnmarshalJSON(b []byte) error {
	type serverJSON struct {
		UDPListen                     string       `json:"udp_listen"`
		StunAltListen                 string       `json:"stun_alt_listen"`
		HTTPListen                    string       `json:"http_listen"`
		DatabasePath                  string       `json:"database_path"`
		AdminPassword                 string       `json:"admin_password"`
		AdminPasswordHash             string       `json:"admin_password_hash"`
		AdminJWTSecret                string       `json:"admin_jwt_secret"`
		AdminAccessTokenTTL           durationJSON `json:"admin_access_token_ttl"`
		AdminRefreshTokenTTL          durationJSON `json:"admin_refresh_token_ttl"`
		AdminAllowedOrigins           []string     `json:"admin_allowed_origins"`
		PSK                           string       `json:"psk"`
		PeerTTL                       durationJSON `json:"peer_ttl"`
		PairTTL                       durationJSON `json:"pair_ttl"`
		RelayIdleTimeout              durationJSON `json:"relay_idle_timeout"`
		AllowRelay                    *bool        `json:"allow_relay"`
		AllowLegacy                   *bool        `json:"allow_legacy"`
		ClientNoUPnP                  *bool        `json:"client_no_upnp"`
		ClientUPnPTimeout             durationJSON `json:"client_upnp_timeout"`
		ClientLogLevel                string       `json:"client_log_level"`
		ClientTrayEnabled             *bool        `json:"client_tray_enabled"`
		ClientPunchTimeout            durationJSON `json:"client_punch_timeout"`
		ClientForceRelay              *bool        `json:"client_force_relay"`
		ClientAllowLegacy             *bool        `json:"client_allow_legacy"`
		ClientReleaseVersion          string       `json:"client_release_version"`
		ClientReleaseURL              string       `json:"client_release_url"`
		ClientReleaseSHA256           string       `json:"client_release_sha256"`
		ClientReleasePublishedAt      string       `json:"client_release_published_at"`
		ClientReleaseNotes            string       `json:"client_release_notes"`
		ClientReleaseMinimumSupported string       `json:"client_release_minimum_supported_version"`
		ClientReleaseFile             string       `json:"client_release_file"`
	}
	var x serverJSON
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	if x.UDPListen != "" {
		s.UDPListen = x.UDPListen
	}
	if x.StunAltListen != "" {
		s.StunAltListen = x.StunAltListen
	}
	if x.HTTPListen != "" {
		s.HTTPListen = x.HTTPListen
	}
	if x.DatabasePath != "" {
		s.DatabasePath = x.DatabasePath
	}
	s.AdminPassword = x.AdminPassword
	s.AdminPasswordHash = x.AdminPasswordHash
	s.AdminJWTSecret = x.AdminJWTSecret
	if x.AdminAccessTokenTTL.set {
		s.AdminAccessTokenTTL = x.AdminAccessTokenTTL.Duration
	}
	if x.AdminRefreshTokenTTL.set {
		s.AdminRefreshTokenTTL = x.AdminRefreshTokenTTL.Duration
	}
	if x.AdminAllowedOrigins != nil {
		s.AdminAllowedOrigins = x.AdminAllowedOrigins
	}
	s.PSK = x.PSK
	if x.PeerTTL.set {
		s.PeerTTL = x.PeerTTL.Duration
	}
	if x.PairTTL.set {
		s.PairTTL = x.PairTTL.Duration
	}
	if x.RelayIdleTimeout.set {
		s.RelayIdleTimeout = x.RelayIdleTimeout.Duration
	}
	if x.AllowRelay != nil {
		s.AllowRelay = *x.AllowRelay
	}
	if x.AllowLegacy != nil {
		s.AllowLegacy = *x.AllowLegacy
	}
	if x.ClientNoUPnP != nil {
		s.ClientNoUPnP = *x.ClientNoUPnP
	}
	if x.ClientUPnPTimeout.set {
		s.ClientUPnPTimeout = x.ClientUPnPTimeout.Duration
	}
	if x.ClientLogLevel != "" {
		s.ClientLogLevel = x.ClientLogLevel
	}
	if x.ClientTrayEnabled != nil {
		s.ClientTrayEnabled = *x.ClientTrayEnabled
	}
	if x.ClientPunchTimeout.set {
		s.ClientPunchTimeout = x.ClientPunchTimeout.Duration
	}
	if x.ClientForceRelay != nil {
		s.ClientForceRelay = *x.ClientForceRelay
	}
	if x.ClientAllowLegacy != nil {
		s.ClientAllowLegacy = *x.ClientAllowLegacy
	}
	if x.ClientReleaseVersion != "" {
		s.ClientReleaseVersion = x.ClientReleaseVersion
	}
	if x.ClientReleaseURL != "" {
		s.ClientReleaseURL = x.ClientReleaseURL
	}
	if x.ClientReleaseSHA256 != "" {
		s.ClientReleaseSHA256 = x.ClientReleaseSHA256
	}
	if x.ClientReleasePublishedAt != "" {
		s.ClientReleasePublishedAt = x.ClientReleasePublishedAt
	}
	if x.ClientReleaseNotes != "" {
		s.ClientReleaseNotes = x.ClientReleaseNotes
	}
	if x.ClientReleaseMinimumSupported != "" {
		s.ClientReleaseMinimumSupported = x.ClientReleaseMinimumSupported
	}
	if x.ClientReleaseFile != "" {
		s.ClientReleaseFile = x.ClientReleaseFile
	}
	return nil
}

func (s Server) MarshalJSON() ([]byte, error) {
	type serverJSON struct {
		UDPListen                     string   `json:"udp_listen"`
		StunAltListen                 string   `json:"stun_alt_listen"`
		HTTPListen                    string   `json:"http_listen"`
		DatabasePath                  string   `json:"database_path"`
		AdminPassword                 string   `json:"admin_password,omitempty"`
		AdminPasswordHash             string   `json:"admin_password_hash,omitempty"`
		AdminJWTSecret                string   `json:"admin_jwt_secret,omitempty"`
		AdminAccessTokenTTL           string   `json:"admin_access_token_ttl"`
		AdminRefreshTokenTTL          string   `json:"admin_refresh_token_ttl"`
		AdminAllowedOrigins           []string `json:"admin_allowed_origins,omitempty"`
		PSK                           string   `json:"psk"`
		PeerTTL                       string   `json:"peer_ttl"`
		PairTTL                       string   `json:"pair_ttl"`
		RelayIdleTimeout              string   `json:"relay_idle_timeout"`
		AllowRelay                    bool     `json:"allow_relay"`
		AllowLegacy                   bool     `json:"allow_legacy"`
		ClientNoUPnP                  bool     `json:"client_no_upnp"`
		ClientUPnPTimeout             string   `json:"client_upnp_timeout"`
		ClientLogLevel                string   `json:"client_log_level"`
		ClientTrayEnabled             bool     `json:"client_tray_enabled"`
		ClientPunchTimeout            string   `json:"client_punch_timeout"`
		ClientForceRelay              bool     `json:"client_force_relay"`
		ClientAllowLegacy             bool     `json:"client_allow_legacy"`
		ClientReleaseVersion          string   `json:"client_release_version,omitempty"`
		ClientReleaseURL              string   `json:"client_release_url,omitempty"`
		ClientReleaseSHA256           string   `json:"client_release_sha256,omitempty"`
		ClientReleasePublishedAt      string   `json:"client_release_published_at,omitempty"`
		ClientReleaseNotes            string   `json:"client_release_notes,omitempty"`
		ClientReleaseMinimumSupported string   `json:"client_release_minimum_supported_version,omitempty"`
		ClientReleaseFile             string   `json:"client_release_file,omitempty"`
	}
	return json.Marshal(serverJSON{
		UDPListen:                     s.UDPListen,
		StunAltListen:                 s.StunAltListen,
		HTTPListen:                    s.HTTPListen,
		DatabasePath:                  s.DatabasePath,
		AdminPassword:                 s.AdminPassword,
		AdminPasswordHash:             s.AdminPasswordHash,
		AdminJWTSecret:                s.AdminJWTSecret,
		AdminAccessTokenTTL:           s.AdminAccessTokenTTL.String(),
		AdminRefreshTokenTTL:          s.AdminRefreshTokenTTL.String(),
		AdminAllowedOrigins:           s.AdminAllowedOrigins,
		PSK:                           s.PSK,
		PeerTTL:                       s.PeerTTL.String(),
		PairTTL:                       s.PairTTL.String(),
		RelayIdleTimeout:              s.RelayIdleTimeout.String(),
		AllowRelay:                    s.AllowRelay,
		AllowLegacy:                   s.AllowLegacy,
		ClientNoUPnP:                  s.ClientNoUPnP,
		ClientUPnPTimeout:             s.ClientUPnPTimeout.String(),
		ClientLogLevel:                s.ClientLogLevel,
		ClientTrayEnabled:             s.ClientTrayEnabled,
		ClientPunchTimeout:            s.ClientPunchTimeout.String(),
		ClientForceRelay:              s.ClientForceRelay,
		ClientAllowLegacy:             s.ClientAllowLegacy,
		ClientReleaseVersion:          s.ClientReleaseVersion,
		ClientReleaseURL:              s.ClientReleaseURL,
		ClientReleaseSHA256:           s.ClientReleaseSHA256,
		ClientReleasePublishedAt:      s.ClientReleasePublishedAt,
		ClientReleaseNotes:            s.ClientReleaseNotes,
		ClientReleaseMinimumSupported: s.ClientReleaseMinimumSupported,
		ClientReleaseFile:             s.ClientReleaseFile,
	})
}

func (c *Client) UnmarshalJSON(b []byte) error {
	type clientJSON struct {
		Server       string       `json:"server"`
		ServerHTTP   string       `json:"server_http"`
		DeviceID     string       `json:"device_id"`
		DeviceName   string       `json:"device_name"`
		PeerID       string       `json:"peer_id"`
		PSK          string       `json:"psk"`
		NoUPnP       *bool        `json:"no_upnp"`
		UPnPTimeout  durationJSON `json:"upnp_timeout"`
		LogLevel     string       `json:"log_level"`
		TrayEnabled  *bool        `json:"tray_enabled"`
		PunchTimeout durationJSON `json:"punch_timeout"`
		ForceRelay   *bool        `json:"force_relay"`
		AllowLegacy  *bool        `json:"allow_legacy"`
		Forwards     []string     `json:"forwards"`
	}
	var x clientJSON
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	c.Server = x.Server
	c.ServerHTTP = x.ServerHTTP
	c.DeviceID = x.DeviceID
	c.DeviceName = x.DeviceName
	c.PeerID = x.PeerID
	c.PSK = x.PSK
	if x.NoUPnP != nil {
		c.NoUPnP = *x.NoUPnP
	}
	if x.UPnPTimeout.set {
		c.UPnPTimeout = x.UPnPTimeout.Duration
	}
	if x.LogLevel != "" {
		c.LogLevel = x.LogLevel
	}
	if x.TrayEnabled != nil {
		c.TrayEnabled = *x.TrayEnabled
	}
	if x.PunchTimeout.set {
		c.PunchTimeout = x.PunchTimeout.Duration
	}
	if x.ForceRelay != nil {
		c.ForceRelay = *x.ForceRelay
	}
	if x.AllowLegacy != nil {
		c.AllowLegacy = *x.AllowLegacy
	}
	c.Forwards = x.Forwards
	return nil
}

func (c Client) MarshalJSON() ([]byte, error) {
	type clientJSON struct {
		Server       string   `json:"server"`
		ServerHTTP   string   `json:"server_http"`
		DeviceID     string   `json:"device_id,omitempty"`
		DeviceName   string   `json:"device_name,omitempty"`
		PeerID       string   `json:"peer_id,omitempty"`
		PSK          string   `json:"psk"`
		NoUPnP       bool     `json:"no_upnp"`
		UPnPTimeout  string   `json:"upnp_timeout"`
		LogLevel     string   `json:"log_level"`
		TrayEnabled  bool     `json:"tray_enabled"`
		PunchTimeout string   `json:"punch_timeout"`
		ForceRelay   bool     `json:"force_relay"`
		AllowLegacy  bool     `json:"allow_legacy"`
		Forwards     []string `json:"forwards,omitempty"`
	}
	return json.Marshal(clientJSON{
		Server:       c.Server,
		ServerHTTP:   c.ServerHTTP,
		DeviceID:     c.DeviceID,
		DeviceName:   c.DeviceName,
		PeerID:       c.PeerID,
		PSK:          c.PSK,
		NoUPnP:       c.NoUPnP,
		UPnPTimeout:  c.UPnPTimeout.String(),
		LogLevel:     c.LogLevel,
		TrayEnabled:  c.TrayEnabled,
		PunchTimeout: c.PunchTimeout.String(),
		ForceRelay:   c.ForceRelay,
		AllowLegacy:  c.AllowLegacy,
		Forwards:     c.Forwards,
	})
}

type durationJSON struct {
	time.Duration
	set bool
}

func (d *durationJSON) UnmarshalJSON(b []byte) error {
	d.set = true
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		d.Duration = v
		return nil
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return err
	}
	d.Duration = time.Duration(n)
	return nil
}

func LoadJSON(path string, dst any) error {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("decode config %s: %w", path, err)
	}
	return nil
}

func SaveJSON(path string, src any) error {
	if path == "" {
		return errors.New("empty config path")
	}
	b, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, b, "", "  "); err != nil {
		return fmt.Errorf("format config: %w", err)
	}
	pretty.WriteByte('\n')
	if err := os.WriteFile(path, pretty.Bytes(), 0600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

func SaveClientLocalJSON(path string, cfg Client) error {
	if path == "" {
		return errors.New("empty config path")
	}
	local := struct {
		ServerHTTP string `json:"server_http"`
		DeviceID   string `json:"device_id,omitempty"`
		DeviceName string `json:"device_name,omitempty"`
		PSK        string `json:"psk"`
	}{
		ServerHTTP: cfg.ServerHTTP,
		DeviceID:   cfg.DeviceID,
		DeviceName: cfg.DeviceName,
		PSK:        cfg.PSK,
	}
	b, err := json.Marshal(local)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, b, "", "  "); err != nil {
		return fmt.Errorf("format config: %w", err)
	}
	pretty.WriteByte('\n')
	if err := os.WriteFile(path, pretty.Bytes(), 0600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

func DurationFlag(fs *flag.FlagSet, name string, value time.Duration, usage string) *time.Duration {
	return fs.Duration(name, value, usage)
}
