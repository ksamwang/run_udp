package server

import (
	"context"
	"fmt"
	"time"

	"udp_tunnel_demo/internal/store"
)

const lanOnlineTTL = 60 * time.Second

func (a *App) enrichedDevices(ctx context.Context) ([]store.Device, error) {
	devices, err := a.db.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := a.db.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	states, err := a.db.ListTunnelStates(ctx)
	if err != nil {
		return nil, err
	}
	productStates, err := a.db.ListDeviceProductStates(ctx)
	if err != nil {
		return nil, err
	}
	virtualAddresses, err := a.db.ListVirtualAddresses(ctx, 0)
	if err != nil {
		return nil, err
	}
	peerStates, err := a.db.ListVirtualPeerStates(ctx, 0)
	if err != nil {
		return nil, err
	}
	productByDevice := map[string]map[string]store.DeviceProductState{}
	for _, state := range productStates {
		if productByDevice[state.DeviceID] == nil {
			productByDevice[state.DeviceID] = map[string]store.DeviceProductState{}
		}
		productByDevice[state.DeviceID][state.Product] = state
	}
	addressByDevice := map[string]store.VirtualAddress{}
	for _, address := range virtualAddresses {
		addressByDevice[address.DeviceID] = address
	}
	lanByDevice := summarizeLANPeerStates(peerStates)
	stateMap := latestStateByPair(states)
	deviceErrs := map[string]string{}
	for _, st := range states {
		if st.LastError != "" && deviceErrs[st.DeviceID] == "" {
			deviceErrs[st.DeviceID] = st.LastError
		}
	}
	ruleCount := map[string]int{}
	healthy := map[string]bool{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		ruleCount[r.SourceID]++
		ruleCount[r.TargetID]++
		if st, ok := stateMap[pairStateKey(r.SourceID, r.TargetID, r.Profile)]; ok && (st.State == "p2p" || st.State == "relay") {
			healthy[r.SourceID] = true
			healthy[r.TargetID] = true
		}
	}
	for i := range devices {
		switch {
		case ruleCount[devices[i].ID] == 0:
			devices[i].HealthSummary = "无规则"
		case healthy[devices[i].ID]:
			devices[i].HealthSummary = "至少一条隧道正常"
		case hasBackoff(stateMap, devices[i].ID):
			devices[i].HealthSummary = "回退重试中"
		default:
			devices[i].HealthSummary = "有规则但未建链"
		}
		devices[i].LastError = deviceErrs[devices[i].ID]
		applyProductStateToDevice(&devices[i], productByDevice[devices[i].ID], addressByDevice[devices[i].ID], lanByDevice[devices[i].ID], a.currentPeerTTL())
	}
	return devices, nil
}

type lanPeerSummary struct {
	p2pPeers           int
	relayPeers         int
	downPeers          int
	adapterState       string
	selectedCIDR       string
	routeConflict      string
	lastError          string
	activeSessions     int
	hotPaths           int
	socketRotations    uint64
	lastRotationReason string
	updatedAt          string
}

func summarizeLANPeerStates(states []store.VirtualPeerState) map[string]lanPeerSummary {
	out := map[string]lanPeerSummary{}
	for _, state := range states {
		summary := out[state.DeviceID]
		switch state.Path {
		case "p2p":
			summary.p2pPeers++
		case "relay":
			summary.relayPeers++
		default:
			if state.State == "down" || state.Path == "down" || state.Path == "" {
				summary.downPeers++
			}
		}
		if summary.updatedAt == "" || state.UpdatedAt > summary.updatedAt {
			summary.updatedAt = state.UpdatedAt
			summary.adapterState = state.AdapterState
			summary.selectedCIDR = state.SelectedCIDR
			summary.routeConflict = state.RouteConflict
			summary.lastError = state.LastError
			summary.lastRotationReason = state.LastRotationReason
		}
		summary.activeSessions += state.ActiveSessions
		summary.hotPaths += state.HotPaths
		if state.SocketRotations > summary.socketRotations {
			summary.socketRotations = state.SocketRotations
		}
		out[state.DeviceID] = summary
	}
	return out
}

func applyProductStateToDevice(d *store.Device, states map[string]store.DeviceProductState, address store.VirtualAddress, lan lanPeerSummary, agentTTL time.Duration) {
	capabilities := []string{}
	if agent, ok := states["agent"]; ok {
		capabilities = append(capabilities, "Agent")
		d.AgentOnline = agent.Online && recentWithin(agent.LastSeenAt, agentTTL)
		d.LastAgentSeen = agent.LastSeenAt
		d.AgentLastSource = agent.LastSource
	}
	if lanState, ok := states["lan"]; ok {
		if lanState.LastSource == "lan_status" {
			capabilities = append(capabilities, "UDPTunnelLAN")
		}
		d.LastLANSeen = lanState.LastSeenAt
		d.LANLastSource = lanState.LastSource
		d.LANLastError = firstString(lan.lastError, lanState.LastError)
		d.LANOnline = lanState.LastSource == "lan_status" && recentWithin(lanState.LastSeenAt, lanOnlineTTL)
	}
	d.ProductCapabilities = capabilities
	d.LANVirtualIP = address.VirtualIP
	d.LANNetworkID = address.NetworkID
	d.LANAdapterState = lan.adapterState
	d.LANSelectedCIDR = lan.selectedCIDR
	d.LANRouteConflict = lan.routeConflict
	if lan.p2pPeers+lan.relayPeers+lan.downPeers > 0 {
		d.LANPathSummary = fmt.Sprintf("P2P %d / Relay %d / Down %d", lan.p2pPeers, lan.relayPeers, lan.downPeers)
	}
	d.LANActiveSessions = lan.activeSessions
	d.LANHotPaths = lan.hotPaths
	d.LANSocketRotations = lan.socketRotations
	d.LANRotationReason = lan.lastRotationReason
}

func recentWithin(value string, ttl time.Duration) bool {
	if value == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	return time.Since(t) <= ttl
}

func firstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (a *App) enrichedRules(ctx context.Context) ([]store.ForwardRule, error) {
	rules, err := a.db.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := a.db.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	states, err := a.db.ListTunnelStates(ctx)
	if err != nil {
		return nil, err
	}
	deviceMap := map[string]store.Device{}
	for _, d := range devices {
		deviceMap[d.ID] = d
	}
	stateMap := latestStateByPair(states)
	for i := range rules {
		r := &rules[i]
		if !r.Enabled {
			r.RuntimeState = "disabled"
			r.LastError = ""
			r.LastUpdatedAt = r.UpdatedAt
			continue
		}
		src, srcOK := deviceMap[r.SourceID]
		dst, dstOK := deviceMap[r.TargetID]
		switch {
		case !srcOK || !dstOK:
			r.RuntimeState = "down"
			r.LastError = "device_not_found"
			r.LastUpdatedAt = r.UpdatedAt
		case !src.Enabled || !dst.Enabled:
			r.RuntimeState = "down"
			r.LastError = "device_disabled"
			r.LastUpdatedAt = r.UpdatedAt
		default:
			st, ok := stateMap[pairStateKey(r.SourceID, r.TargetID, r.Profile)]
			if !ok {
				r.RuntimeState = "down"
				r.LastError = "session_not_established"
				r.LastUpdatedAt = r.UpdatedAt
				continue
			}
			r.RuntimeState = normalizeRuntimeState(st.State, st.Via)
			r.LastError = st.LastError
			r.LastUpdatedAt = st.UpdatedAt
			r.Attempt = st.Attempt
			r.NextRetryAt = st.NextRetryAt
		}
	}
	return rules, nil
}

func latestStateByPair(states []store.TunnelState) map[string]store.TunnelState {
	out := map[string]store.TunnelState{}
	for _, st := range states {
		key := pairStateKey(st.DeviceID, st.PeerID, st.Profile)
		if _, ok := out[key]; !ok {
			out[key] = st
		}
	}
	return out
}

func pairStateKey(a, b, profile string) string {
	if a <= b {
		return a + "\x00" + b + "\x00" + store.NormalizeProfile(profile)
	}
	return b + "\x00" + a + "\x00" + store.NormalizeProfile(profile)
}

func normalizeRuntimeState(state, via string) string {
	switch state {
	case "p2p", "relay", "connecting", "down", "disabled", "backoff":
		return state
	case "connected":
		if via == "relay" {
			return "relay"
		}
		return "p2p"
	case "stopped", "":
		return "down"
	default:
		if via == "relay" {
			return "relay"
		}
		return state
	}
}

func hasBackoff(states map[string]store.TunnelState, deviceID string) bool {
	for _, st := range states {
		if (st.DeviceID == deviceID || st.PeerID == deviceID) && st.State == "backoff" {
			return true
		}
	}
	return false
}
