package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"udp_tunnel_demo/internal/packet"
	"udp_tunnel_demo/internal/wintun"
)

const (
	lanPacketBatchSize    = 16
	lanPacketPollInterval = 100 * time.Millisecond
)

type lanRelayFrame struct {
	NetworkID int64  `json:"network_id"`
	SrcDevice string `json:"src_device"`
	DstDevice string `json:"dst_device"`
	Type      byte   `json:"type"`
	Payload   string `json:"payload"`
}

func runPacketForwarding(ctx context.Context, serverHTTP string, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager, deviceID string, networkID int64) {
	defer adapter.Close()
	serverHTTP = strings.TrimRight(strings.TrimSpace(serverHTTP), "/")
	log.Printf("LAN packet runtime started: network_id=%d device=%s relay_endpoint=%s", networkID, deviceID, serverHTTP)
	outbound := make(chan packet.RoutedFrame, 256)
	go readWintunPackets(ctx, adapter, router, outbound)
	go sendRelayPackets(ctx, serverHTTP, link, outbound)
	go pollRelayPackets(ctx, serverHTTP, adapter, router, link, deviceID)
	<-ctx.Done()
	log.Printf("LAN packet runtime stopped")
}

func readWintunPackets(ctx context.Context, adapter *wintun.Adapter, router *packet.Router, outbound chan<- packet.RoutedFrame) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		pkt, err := adapter.ReadPacket()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("LAN packet read failed: %v", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		frame, err := router.RouteOutbound(pkt)
		if err != nil {
			if !errors.Is(err, packet.ErrRouteMiss) {
				log.Printf("LAN packet dropped: %v", err)
			}
			continue
		}
		select {
		case outbound <- frame:
		case <-ctx.Done():
			return
		default:
			log.Printf("LAN outbound queue full; drop dst=%s bytes=%d", frame.DstDevice, len(frame.Payload))
		}
	}
}

func sendRelayPackets(ctx context.Context, serverHTTP string, link *packet.LinkManager, outbound <-chan packet.RoutedFrame) {
	batch := make([]packet.RoutedFrame, 0, lanPacketBatchSize)
	timer := time.NewTimer(time.Second)
	if !timer.Stop() {
		<-timer.C
	}
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := postRelayFrames(ctx, serverHTTP, batch); err != nil {
			log.Printf("LAN relay send failed: %v", err)
		} else {
			for _, frame := range batch {
				_, _ = link.Send(frame.DstDevice, frame.Payload)
			}
		}
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-outbound:
			batch = append(batch, frame)
			if len(batch) >= lanPacketBatchSize {
				flush()
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				continue
			}
			timer.Reset(20 * time.Millisecond)
		case <-timer.C:
			flush()
		}
	}
}

func pollRelayPackets(ctx context.Context, serverHTTP string, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager, deviceID string) {
	relayDisabled := false
	for {
		frames, err := pollRelayFrames(ctx, serverHTTP, deviceID, lanPacketBatchSize)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if strings.Contains(err.Error(), "relay_disabled") {
				if !relayDisabled {
					log.Printf("LAN relay disabled by server; packet runtime waits for P2P integration or relay enablement")
				}
				relayDisabled = true
				time.Sleep(15 * time.Second)
			} else {
				relayDisabled = false
				log.Printf("LAN relay poll failed: %v", err)
				time.Sleep(time.Second)
			}
			continue
		}
		relayDisabled = false
		for _, frame := range frames {
			payload, err := base64.StdEncoding.DecodeString(frame.Payload)
			if err != nil {
				log.Printf("LAN relay frame decode failed: src=%s err=%v", frame.SrcDevice, err)
				continue
			}
			routed := packet.RoutedFrame{
				NetworkID: frame.NetworkID, SrcDevice: frame.SrcDevice, DstDevice: frame.DstDevice,
				PacketType: frame.Type, Payload: payload,
			}
			router.RecordInbound(routed)
			_ = link.Receive(frame.SrcDevice, payload, packet.LinkPathRelay)
			if err := adapter.WritePacket(payload); err != nil {
				log.Printf("LAN packet write failed: src=%s bytes=%d err=%v", frame.SrcDevice, len(payload), err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(lanPacketPollInterval):
		}
	}
}

func postRelayFrames(ctx context.Context, serverHTTP string, frames []packet.RoutedFrame) error {
	reqFrames := make([]lanRelayFrame, 0, len(frames))
	deviceID := ""
	for _, frame := range frames {
		if deviceID == "" {
			deviceID = frame.SrcDevice
		}
		reqFrames = append(reqFrames, lanRelayFrame{
			NetworkID: frame.NetworkID, SrcDevice: frame.SrcDevice, DstDevice: frame.DstDevice,
			Type: frame.PacketType, Payload: base64.StdEncoding.EncodeToString(frame.Payload),
		})
	}
	var resp struct {
		Accepted int `json:"accepted"`
	}
	if err := postLANJSON(ctx, serverHTTP+"/api/lan/packets/send", map[string]any{"device_id": deviceID, "frames": reqFrames}, &resp); err != nil {
		return err
	}
	if resp.Accepted == 0 && len(frames) > 0 {
		return fmt.Errorf("relay accepted no frames")
	}
	return nil
}

func pollRelayFrames(ctx context.Context, serverHTTP, deviceID string, max int) ([]lanRelayFrame, error) {
	var resp struct {
		Frames []lanRelayFrame `json:"frames"`
	}
	err := postLANJSON(ctx, serverHTTP+"/api/lan/packets/poll", map[string]any{"device_id": deviceID, "max": max}, &resp)
	return resp.Frames, err
}

func postLANJSON(ctx context.Context, url string, reqBody any, out any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var apiErr struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Code != "" {
			return fmt.Errorf("http %d %s: %s", resp.StatusCode, apiErr.Code, apiErr.Error)
		}
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
