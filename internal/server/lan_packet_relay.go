package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	lanPacketMaxBatch   = 64
	lanPacketMaxPayload = 64 * 1024
	lanPacketPollTick   = 10 * time.Millisecond
)

type lanPacketRelayFrame struct {
	NetworkID int64  `json:"network_id"`
	SrcDevice string `json:"src_device"`
	DstDevice string `json:"dst_device"`
	Type      byte   `json:"type"`
	Payload   string `json:"payload"`
	QueuedAt  string `json:"queued_at,omitempty"`
}

type lanPacketRelay struct {
	mu       sync.Mutex
	limit    int
	byDevice map[string][]lanPacketRelayFrame
	dropped  uint64
}

func newLANPacketRelay(limit int) *lanPacketRelay {
	if limit <= 0 {
		limit = 256
	}
	return &lanPacketRelay{limit: limit, byDevice: map[string][]lanPacketRelayFrame{}}
}

func (r *lanPacketRelay) Enqueue(frame lanPacketRelayFrame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	frame.QueuedAt = time.Now().Format(time.RFC3339Nano)
	q := append(r.byDevice[frame.DstDevice], frame)
	if len(q) > r.limit {
		r.dropped += uint64(len(q) - r.limit)
		q = q[len(q)-r.limit:]
	}
	r.byDevice[frame.DstDevice] = q
}

func (r *lanPacketRelay) Poll(deviceID string, max int) ([]lanPacketRelayFrame, int, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.byDevice[deviceID]
	if len(q) == 0 {
		return nil, 0, r.dropped
	}
	if max <= 0 || max > lanPacketMaxBatch {
		max = lanPacketMaxBatch
	}
	if max > len(q) {
		max = len(q)
	}
	out := append([]lanPacketRelayFrame(nil), q[:max]...)
	if max == len(q) {
		delete(r.byDevice, deviceID)
	} else {
		r.byDevice[deviceID] = append([]lanPacketRelayFrame(nil), q[max:]...)
	}
	return out, len(r.byDevice[deviceID]), r.dropped
}

func (a *App) handleLANPacketSend(w http.ResponseWriter, r *http.Request) {
	if !a.currentLANAllowRelay() {
		writeJSONOrError(w, nil, badRequest("relay_disabled", "LAN packet relay is disabled"))
		return
	}
	var req struct {
		DeviceID string                `json:"device_id"`
		Frames   []lanPacketRelayFrame `json:"frames"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DeviceID) == "" {
		writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
		return
	}
	accepted := 0
	for _, frame := range req.Frames {
		if frame.SrcDevice == "" {
			frame.SrcDevice = req.DeviceID
		}
		if frame.SrcDevice != req.DeviceID || frame.DstDevice == "" || frame.NetworkID <= 0 {
			continue
		}
		payload, err := base64.StdEncoding.DecodeString(frame.Payload)
		if err != nil || len(payload) == 0 || len(payload) > lanPacketMaxPayload {
			continue
		}
		frame.Payload = base64.StdEncoding.EncodeToString(payload)
		a.lanRelay.Enqueue(frame)
		a.totalRelayed.Add(uint64(len(payload)))
		accepted++
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": accepted})
}

func (a *App) handleLANPacketPoll(w http.ResponseWriter, r *http.Request) {
	if !a.currentLANAllowRelay() {
		writeJSONOrError(w, nil, badRequest("relay_disabled", "LAN packet relay is disabled"))
		return
	}
	var req struct {
		DeviceID string `json:"device_id"`
		Max      int    `json:"max"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DeviceID) == "" {
		writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
		return
	}
	deadline, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(lanPacketPollTick)
	defer ticker.Stop()
	for {
		frames, remaining, dropped := a.lanRelay.Poll(req.DeviceID, req.Max)
		if len(frames) > 0 {
			if remaining > 0 || dropped > 0 || len(frames) >= lanPacketMaxBatch {
				log.Printf("LAN relay poll: device=%s frames=%d remaining=%d dropped=%d", req.DeviceID, len(frames), remaining, dropped)
			}
			writeJSON(w, http.StatusOK, map[string]any{"frames": frames})
			return
		}
		select {
		case <-deadline.Done():
			writeJSON(w, http.StatusOK, map[string]any{"frames": []lanPacketRelayFrame{}})
			return
		case <-ticker.C:
		}
	}
}
