package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"udp_tunnel_demo/internal/store"
)

const lanBootstrapVersion = 1

type lanBootstrapRequest struct {
	DeviceID     string   `json:"device_id"`
	DeviceName   string   `json:"device_name"`
	PublicKey    string   `json:"public_key"`
	Capabilities []string `json:"capabilities"`
}

type lanBootstrapResponse struct {
	Version       int                        `json:"version"`
	Capabilities  []string                   `json:"capabilities"`
	ConfigVersion string                     `json:"config_version"`
	Server        string                     `json:"server"`
	STUNAltPort   int                        `json:"stun_alt_port"`
	RelayEnabled  bool                       `json:"relay_enabled"`
	DeviceID      string                     `json:"device_id"`
	DeviceName    string                     `json:"device_name"`
	Network       store.VirtualNetwork       `json:"network"`
	Address       store.VirtualAddress       `json:"address"`
	Routes        []store.VirtualRoute       `json:"routes"`
	ACL           []store.VirtualACLRule     `json:"acl"`
	Peers         []lanBootstrapPeer         `json:"peers"`
	LearnedPaths  []store.VirtualLearnedPath `json:"learned_paths"`
}

type lanBootstrapPeer struct {
	DeviceID  string `json:"device_id"`
	VirtualIP string `json:"virtual_ip"`
	Hostname  string `json:"hostname"`
	PublicKey string `json:"public_key"`
}

type lanStatusReport struct {
	store.VirtualPeerState
	LearnedPaths []store.VirtualLearnedPath `json:"learned_paths,omitempty"`
}

func requestLANBootstrap(ctx context.Context, serverHTTP string, req lanBootstrapRequest) (lanBootstrapResponse, error) {
	var out lanBootstrapResponse
	serverHTTP = strings.TrimRight(strings.TrimSpace(serverHTTP), "/")
	if serverHTTP == "" {
		return out, fmt.Errorf("server_http is empty")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return out, fmt.Errorf("encode bootstrap: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, serverHTTP+"/api/lan/bootstrap", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return out, fmt.Errorf("LAN bootstrap failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("LAN bootstrap http %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("decode LAN bootstrap: %w", err)
	}
	if out.Version > lanBootstrapVersion {
		return out, fmt.Errorf("unsupported LAN bootstrap version %d", out.Version)
	}
	return out, nil
}

func reportLANStatus(ctx context.Context, serverHTTP string, state store.VirtualPeerState) error {
	return reportLANStatusPayload(ctx, serverHTTP, lanStatusReport{VirtualPeerState: state})
}

func reportLANStatusPayload(ctx context.Context, serverHTTP string, report lanStatusReport) error {
	serverHTTP = strings.TrimRight(strings.TrimSpace(serverHTTP), "/")
	if serverHTTP == "" {
		return fmt.Errorf("server_http is empty")
	}
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode LAN status: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, serverHTTP+"/api/lan/status", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("LAN status failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("LAN status http %d", resp.StatusCode)
	}
	return nil
}
