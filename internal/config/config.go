package config

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	UDPListen                     string        `json:"udp_listen"`
	StunAltListen                 string        `json:"stun_alt_listen"`
	HTTPListen                    string        `json:"http_listen"`
	ControlDatabaseDSN            string        `json:"control_database_dsn"`
	AdminJWTSecret                string        `json:"admin_jwt_secret"`
	AdminAccessTokenTTL           time.Duration `json:"admin_access_token_ttl"`
	AdminRefreshTokenTTL          time.Duration `json:"admin_refresh_token_ttl"`
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

func (c *Client) UnmarshalJSON(b []byte) error {
	type clientJSON struct {
		Server       string       `json:"server"`
		ServerHTTP   string       `json:"server_http"`
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

func LoadServerEnv(path string, cfg *Server) error {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read env %s: %w", path, err)
	}
	defer f.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("decode env %s: bad line %q", path, line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read env %s: %w", path, err)
	}
	return applyServerEnv(values, cfg)
}

func applyServerEnv(values map[string]string, s *Server) error {
	setString := func(key string, dst *string) {
		if v, ok := values[key]; ok {
			*dst = v
		}
	}
	setDuration := func(key string, dst *time.Duration) error {
		v, ok := values[key]
		if !ok || v == "" {
			return nil
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*dst = d
		return nil
	}
	setString("UDP_LISTEN", &s.UDPListen)
	setString("STUN_ALT_LISTEN", &s.StunAltListen)
	setString("HTTP_LISTEN", &s.HTTPListen)
	setString("CONTROL_DATABASE_DSN", &s.ControlDatabaseDSN)
	setString("ADMIN_JWT_SECRET", &s.AdminJWTSecret)
	setString("PSK", &s.PSK)
	for _, item := range []struct {
		key string
		dst *time.Duration
	}{
		{"ADMIN_ACCESS_TOKEN_TTL", &s.AdminAccessTokenTTL},
		{"ADMIN_REFRESH_TOKEN_TTL", &s.AdminRefreshTokenTTL},
	} {
		if err := setDuration(item.key, item.dst); err != nil {
			return err
		}
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
		DeviceName string `json:"device_name,omitempty"`
		PSK        string `json:"psk"`
	}{
		ServerHTTP: cfg.ServerHTTP,
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
