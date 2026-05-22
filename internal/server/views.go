package server

import (
	"context"

	"udp_tunnel_demo/internal/store"
)

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
	}
	return devices, nil
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
