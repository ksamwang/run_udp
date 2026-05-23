package lan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	ServerHTTP string `json:"server_http"`
	LogLevel   string `json:"log_level,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read LAN config %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("decode LAN config %s: %w", path, err)
	}
	cfg.ServerHTTP = strings.TrimSpace(cfg.ServerHTTP)
	cfg.LogLevel = strings.TrimSpace(cfg.LogLevel)
	return cfg, nil
}

func SaveConfig(path string, cfg Config) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("empty LAN config path")
	}
	cfg.ServerHTTP = strings.TrimSpace(cfg.ServerHTTP)
	cfg.LogLevel = strings.TrimSpace(cfg.LogLevel)
	b, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode LAN config: %w", err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, b, "", "  "); err != nil {
		return fmt.Errorf("format LAN config: %w", err)
	}
	pretty.WriteByte('\n')
	if err := os.WriteFile(path, pretty.Bytes(), 0600); err != nil {
		return fmt.Errorf("write LAN config %s: %w", path, err)
	}
	return nil
}
