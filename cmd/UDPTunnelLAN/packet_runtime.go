package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"udp_tunnel_demo/internal/lan"
	"udp_tunnel_demo/internal/packet"
	"udp_tunnel_demo/internal/protocol"
	"udp_tunnel_demo/internal/store"
	"udp_tunnel_demo/internal/upnp"
	"udp_tunnel_demo/internal/wintun"

	"golang.org/x/crypto/curve25519"
)

const (
	lanPacketBatchSize    = 16
	lanPacketPollInterval = 100 * time.Millisecond
	lanConfigRefreshEvery = 10 * time.Second
	lanUPnPTimeout        = 4 * time.Second
)

type lanRelayFrame struct {
	NetworkID int64  `json:"network_id"`
	SrcDevice string `json:"src_device"`
	DstDevice string `json:"dst_device"`
	Type      byte   `json:"type"`
	Payload   string `json:"payload"`
}

func runPacketForwarding(ctx context.Context, serverHTTP string, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager, resp lanBootstrapResponse, identity lan.Identity, deviceID string, networkID int64) {
	defer adapter.Close()
	serverHTTP = strings.TrimRight(strings.TrimSpace(serverHTTP), "/")
	log.Printf("LAN packet runtime started: network_id=%d device=%s relay_endpoint=%s", networkID, deviceID, serverHTTP)
	outbound := make(chan packet.RoutedFrame, 256)
	p2p := startLANP2P(ctx, resp.Server, adapter, router, link, resp, identity, deviceID)
	go readWintunPackets(ctx, adapter, router, outbound)
	go sendPackets(ctx, serverHTTP, link, p2p, outbound)
	go pollRelayPackets(ctx, serverHTTP, adapter, router, link, deviceID)
	go refreshLANPeers(ctx, serverHTTP, router, link, p2p, resp, identity, deviceID)
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
			if !isWintunNoPacketError(err) {
				log.Printf("LAN packet read failed: %v", err)
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if !isSupportedTunPacket(pkt) {
			continue
		}
		frame, err := router.RouteOutbound(pkt)
		if err != nil {
			if !errors.Is(err, packet.ErrRouteMiss) && !errors.Is(err, packet.ErrUnsupportedProtocol) && !errors.Is(err, packet.ErrInvalidIPv4) {
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

func isSupportedTunPacket(pkt []byte) bool {
	if len(pkt) == 0 {
		return false
	}
	version := pkt[0] >> 4
	return version == 4
}

func isWintunNoPacketError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no more data") || strings.Contains(msg, "没有更多数据")
}

func sendPackets(ctx context.Context, serverHTTP string, link *packet.LinkManager, p2p *lanP2P, outbound <-chan packet.RoutedFrame) {
	batch := make([]packet.RoutedFrame, 0, lanPacketBatchSize)
	timer := time.NewTimer(time.Second)
	if !timer.Stop() {
		<-timer.C
	}
	flush := func() {
		if len(batch) == 0 {
			return
		}
		relay := batch[:0]
		for _, frame := range batch {
			if p2p != nil && p2p.Send(frame) == nil {
				_, _ = link.Send(frame.DstDevice, frame.Payload)
				continue
			}
			relay = append(relay, frame)
		}
		batch = relay
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

type lanP2P struct {
	conn        *net.UDPConn
	peers       map[string]*lanP2PPeer
	peerMu      sync.RWMutex
	server      *net.UDPAddr
	deviceID    string
	x25519Priv  [32]byte
	x25519Pub   string
	upnpAddr    string
	registering map[string]bool
}

type lanP2PPeer struct {
	id        string
	publicKey string
	x25519Pub string
	addr      atomic.Pointer[net.UDPAddr]
	upnpAddr  atomic.Pointer[net.UDPAddr]
	punched   atomic.Bool
	punching  atomic.Bool
	registers atomic.Uint64
	tx        *packet.Codec
	rx        *packet.Codec
}

func startLANP2P(ctx context.Context, serverAddr string, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager, resp lanBootstrapResponse, identity lan.Identity, deviceID string) *lanP2P {
	server, err := net.ResolveUDPAddr("udp", strings.TrimSpace(serverAddr))
	if err != nil || server == nil {
		log.Printf("LAN P2P disabled: bad server addr %q: %v", serverAddr, err)
		return nil
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Printf("LAN P2P disabled: local socket failed: %v", err)
		return nil
	}
	priv, pub, err := newX25519Keypair()
	if err != nil {
		log.Printf("LAN P2P disabled: x25519 keypair failed: %v", err)
		_ = conn.Close()
		return nil
	}
	upnpMapping, upnpAddr := tryLANUPnP(ctx, conn, deviceID)
	p := &lanP2P{
		conn: conn, server: server, deviceID: deviceID, x25519Priv: priv, x25519Pub: pub,
		upnpAddr: upnpAddr, peers: map[string]*lanP2PPeer{}, registering: map[string]bool{},
	}
	go func() {
		<-ctx.Done()
		_ = upnpMapping.Close()
		_ = conn.Close()
	}()
	if upnpMapping != nil {
		go refreshLANUPnPMapping(ctx, deviceID, upnpMapping)
	}
	go p.readLoop(ctx, adapter, router, link)
	p.UpsertPeers(ctx, identity, resp.Peers)
	log.Printf("LAN P2P started: socket=%s server=%s upnp=%q peers=%d", conn.LocalAddr(), server, upnpAddr, len(resp.Peers))
	return p
}

func (p *lanP2P) UpsertPeers(ctx context.Context, identity lan.Identity, peers []lanBootstrapPeer) {
	if p == nil {
		return
	}
	for _, peer := range peers {
		peerID := strings.TrimSpace(peer.DeviceID)
		if peerID == "" || peerID == p.deviceID {
			continue
		}
		shouldRegister := false
		p.peerMu.Lock()
		if existing := p.peers[peerID]; existing == nil {
			p.peers[peerID] = &lanP2PPeer{id: peerID, publicKey: peer.PublicKey}
			shouldRegister = true
		} else {
			existing.publicKey = peer.PublicKey
			if !p.registering[peerID] {
				shouldRegister = true
			}
		}
		if shouldRegister {
			p.registering[peerID] = true
		}
		p.peerMu.Unlock()
		if shouldRegister {
			log.Printf("LAN P2P register loop starting: peer=%s virtual_ip=%s public_key_set=%v", peerID, peer.VirtualIP, strings.TrimSpace(peer.PublicKey) != "")
			go p.registerLoop(ctx, identity, peerID)
		}
	}
}

func (p *lanP2P) Send(frame packet.RoutedFrame) error {
	peer := p.peer(frame.DstDevice)
	if peer == nil || !peer.punched.Load() {
		return packet.ErrLinkUnavailable
	}
	addr := peer.addr.Load()
	if addr == nil {
		return packet.ErrLinkUnavailable
	}
	payload := frame.Payload
	if peer.tx != nil {
		sealed, err := peer.tx.Seal(frame.Payload)
		if err != nil {
			return err
		}
		payload = sealed
	}
	_, err := p.conn.WriteToUDP(payload, addr)
	return err
}

func (p *lanP2P) peer(id string) *lanP2PPeer {
	p.peerMu.RLock()
	defer p.peerMu.RUnlock()
	return p.peers[id]
}

func (p *lanP2P) registerLoop(ctx context.Context, identity lan.Identity, peerID string) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		p.writeLANRegister(identity, peerID)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *lanP2P) writeLANRegister(identity lan.Identity, peerID string) {
	peer := p.peer(peerID)
	if peer == nil {
		return
	}
	ts := time.Now().Unix()
	profile := store.ProfileLANPacket
	sig, err := lan.SignRegisterPayload(identity, p.deviceID, peerID, profile, ts, p.x25519Pub)
	if err != nil {
		log.Printf("LAN P2P register sign failed: peer=%s err=%v", peerID, err)
		return
	}
	msg := &protocol.Message{
		Type: protocol.MsgLANRegister, From: p.deviceID, Peer: peerID, Profile: profile,
		UpnpAddr: p.upnpAddr, Payload: p.x25519Pub, Timestamp: ts, Signature: sig,
	}
	sent := peer.registers.Add(1)
	if sent == 1 || sent%10 == 0 {
		log.Printf("LAN P2P registering: peer=%s server=%s sent=%d", peerID, p.server, sent)
	}
	p.writeControl(p.server, msg)
}

func (p *lanP2P) startPunchLoop(ctx context.Context, peer *lanP2PPeer) {
	if peer == nil || !peer.punching.CompareAndSwap(false, true) {
		return
	}
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		attempts := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if peer.punched.Load() {
				return
			}
			addr := peer.addr.Load()
			if addr == nil {
				continue
			}
			msg := &protocol.Message{Type: protocol.MsgPunch, From: p.deviceID, Profile: store.ProfileLANPacket}
			p.writeControl(addr, msg)
			upnpAddr := peer.upnpAddr.Load()
			if upnpAddr != nil && upnpAddr.String() != addr.String() {
				p.writeControl(upnpAddr, msg)
			}
			attempts++
			if attempts%6 == 0 {
				log.Printf("LAN P2P punching: peer=%s sent=%d target=%s upnp=%v", peer.id, attempts, addr, upnpAddr)
			}
		}
	}()
}

func (p *lanP2P) writeControl(dst *net.UDPAddr, msg *protocol.Message) {
	b, _ := protocol.Encode(msg)
	if _, err := p.conn.WriteToUDP(b, dst); err != nil {
		log.Printf("LAN P2P control send failed: dst=%s err=%v", dst, err)
	}
}

func (p *lanP2P) readLoop(ctx context.Context, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager) {
	buf := make([]byte, 65535)
	for {
		n, src, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("LAN P2P read stopped: %v", err)
			}
			return
		}
		data := append([]byte(nil), buf[:n]...)
		if len(data) > 0 && data[0] == '{' {
			p.handleControl(ctx, data, src, link)
			continue
		}
		peerID := p.peerIDByAddr(src)
		if peerID == "" {
			if src.String() == p.server.String() {
				log.Printf("LAN P2P ignored non-json control from server: bytes=%d", len(data))
			}
			continue
		}
		peer := p.peer(peerID)
		if peer != nil && peer.rx != nil {
			plain, err := peer.rx.Open(data)
			if err != nil {
				log.Printf("LAN P2P packet decrypt failed: peer=%s err=%v", peerID, err)
				continue
			}
			data = plain
		}
		router.RecordInbound(packet.RoutedFrame{SrcDevice: peerID, DstDevice: p.deviceID, Payload: data, PacketType: packet.TypeIPv4})
		_ = link.Receive(peerID, data, packet.LinkPathP2P)
		if err := adapter.WritePacket(data); err != nil {
			log.Printf("LAN P2P write packet failed: peer=%s bytes=%d err=%v", peerID, len(data), err)
		}
	}
}

func (p *lanP2P) ensurePeerCodecs(peer *lanP2PPeer) {
	if peer.tx != nil && peer.rx != nil {
		return
	}
	if strings.TrimSpace(peer.x25519Pub) == "" {
		return
	}
	tx, rx, err := lanPeerCodecsFromX25519(p.x25519Priv, peer.x25519Pub, p.deviceID, peer.id)
	if err != nil {
		log.Printf("LAN P2P codec init failed: peer=%s err=%v", peer.id, err)
		return
	}
	peer.tx = tx
	peer.rx = rx
}

func lanPeerCodecsFromX25519(selfPriv [32]byte, peerPub string, selfID, peerID string) (*packet.Codec, *packet.Codec, error) {
	peerRaw, err := base64.StdEncoding.DecodeString(peerPub)
	if err != nil || len(peerRaw) != 32 {
		return nil, nil, fmt.Errorf("bad peer x25519 public key")
	}
	secret, err := curve25519.X25519(selfPriv[:], peerRaw)
	if err != nil {
		return nil, nil, err
	}
	keys, err := packet.DeriveSessionKeys(secret, 0, "x25519-v1", selfID, peerID)
	if err != nil {
		return nil, nil, err
	}
	if selfID < peerID {
		tx, err := packet.NewCodec(keys.AB, 0, packet.TypeIPv4)
		if err != nil {
			return nil, nil, err
		}
		rx, err := packet.NewCodec(keys.BA, 0, packet.TypeIPv4)
		return tx, rx, err
	}
	tx, err := packet.NewCodec(keys.BA, 0, packet.TypeIPv4)
	if err != nil {
		return nil, nil, err
	}
	rx, err := packet.NewCodec(keys.AB, 0, packet.TypeIPv4)
	return tx, rx, err
}

func newX25519Keypair() ([32]byte, string, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return priv, "", err
	}
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return priv, "", err
	}
	return priv, base64.StdEncoding.EncodeToString(pub), nil
}

func tryLANUPnP(ctx context.Context, conn *net.UDPConn, deviceID string) (*upnp.Mapping, string) {
	if conn == nil || conn.LocalAddr() == nil {
		return nil, ""
	}
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.Port <= 0 {
		return nil, ""
	}
	mapCtx, cancel := context.WithTimeout(ctx, lanUPnPTimeout+time.Second)
	defer cancel()
	mapping, err := upnp.Try(mapCtx, local.Port, fmt.Sprintf("udp-tunnel-lan %s", deviceID), lanUPnPTimeout)
	if err != nil {
		log.Printf("LAN P2P upnp failed: %v", err)
		return nil, ""
	}
	log.Printf("LAN P2P upnp mapped: %s -> :%d", mapping.External(), mapping.InternalPort)
	return mapping, mapping.External()
}

func refreshLANUPnPMapping(ctx context.Context, deviceID string, mapping *upnp.Mapping) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := mapping.Refresh(refreshCtx)
		cancel()
		if err != nil {
			log.Printf("LAN P2P UPnP refresh failed: device=%s err=%v", deviceID, err)
			continue
		}
		log.Printf("LAN P2P UPnP mapping refreshed: device=%s external=%s", deviceID, mapping.External())
	}
}

func (p *lanP2P) handleControl(ctx context.Context, data []byte, src *net.UDPAddr, link *packet.LinkManager) {
	msg, err := protocol.Decode(data)
	if err != nil {
		return
	}
	switch msg.Type {
	case protocol.MsgPeerInfo:
		if store.NormalizeProfile(msg.Profile) != store.ProfileLANPacket {
			return
		}
		peer := p.peer(msg.Peer)
		if peer == nil {
			log.Printf("LAN P2P peer info ignored: unknown peer=%s addr=%s", msg.Peer, msg.Addr)
			return
		}
		peer.x25519Pub = msg.Payload
		p.ensurePeerCodecs(peer)
		addr, err := net.ResolveUDPAddr("udp", msg.Addr)
		if err != nil {
			log.Printf("LAN P2P bad peer addr: peer=%s addr=%q err=%v", msg.Peer, msg.Addr, err)
			return
		}
		peer.addr.Store(addr)
		if strings.TrimSpace(msg.UpnpAddr) != "" {
			upnpAddr, err := net.ResolveUDPAddr("udp", msg.UpnpAddr)
			if err != nil {
				log.Printf("LAN P2P bad peer upnp addr: peer=%s addr=%q err=%v", msg.Peer, msg.UpnpAddr, err)
			} else {
				peer.upnpAddr.Store(upnpAddr)
			}
		}
		if peer.tx != nil && peer.rx != nil {
			_, _ = link.UpsertPeer(packet.PeerEndpoint{DeviceID: peer.id, Addr: addr.String()}, packet.PeerEndpoint{Addr: "http-relay"}, true)
		}
		log.Printf("LAN P2P peer info: peer=%s addr=%s upnp=%q codec_ready=%v", peer.id, addr, msg.UpnpAddr, peer.tx != nil && peer.rx != nil)
		p.startPunchLoop(ctx, peer)
	case protocol.MsgPunch:
		if store.NormalizeProfile(msg.Profile) != store.ProfileLANPacket {
			return
		}
		peer := p.peer(msg.From)
		if peer == nil {
			log.Printf("LAN P2P punch ignored: unknown peer=%s addr=%s", msg.From, src)
			return
		}
		peer.addr.Store(cloneUDPAddr(src))
		if peer.punched.CompareAndSwap(false, true) {
			log.Printf("LAN P2P punched via incoming punch: peer=%s addr=%s", peer.id, src)
		}
		_, _ = link.UpsertPeer(packet.PeerEndpoint{DeviceID: peer.id, Addr: src.String()}, packet.PeerEndpoint{Addr: "http-relay"}, true)
		p.writeControl(src, &protocol.Message{Type: protocol.MsgPunchAck, From: p.deviceID, Profile: store.ProfileLANPacket})
	case protocol.MsgPunchAck:
		if store.NormalizeProfile(msg.Profile) != store.ProfileLANPacket {
			return
		}
		peer := p.peer(msg.From)
		if peer == nil {
			log.Printf("LAN P2P punch ack ignored: unknown peer=%s addr=%s", msg.From, src)
			return
		}
		peer.addr.Store(cloneUDPAddr(src))
		if peer.punched.CompareAndSwap(false, true) {
			log.Printf("LAN P2P punched via ack: peer=%s addr=%s", peer.id, src)
		}
		_, _ = link.UpsertPeer(packet.PeerEndpoint{DeviceID: peer.id, Addr: src.String()}, packet.PeerEndpoint{Addr: "http-relay"}, true)
	}
}

func (p *lanP2P) peerIDByAddr(addr *net.UDPAddr) string {
	p.peerMu.RLock()
	defer p.peerMu.RUnlock()
	for id, peer := range p.peers {
		known := peer.addr.Load()
		if known != nil && known.String() == addr.String() {
			return id
		}
	}
	return ""
}

func refreshLANPeers(ctx context.Context, serverHTTP string, router *packet.Router, link *packet.LinkManager, p2p *lanP2P, initial lanBootstrapResponse, identity lan.Identity, deviceID string) {
	lastConfig := initial.ConfigVersion
	ticker := time.NewTicker(lanConfigRefreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		resp, err := requestLANBootstrap(ctx, serverHTTP, lanBootstrapRequest{
			DeviceID: deviceID, DeviceName: defaultDeviceName(), PublicKey: identity.PublicKey,
			Capabilities: []string{"ipv4", "tcp", "rdp", "wintun"},
		})
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("LAN config refresh failed: %v", err)
			}
			continue
		}
		addresses, available := runtimeAddresses(resp)
		router.UpdateConfig(addresses, resp.ACL, available)
		upsertLinkPeers(link, resp.Peers, false)
		if p2p != nil {
			p2p.UpsertPeers(ctx, identity, resp.Peers)
		}
		if resp.ConfigVersion != lastConfig {
			log.Printf("LAN config refreshed: config_version=%q peers=%d acl=%d routes=%d", resp.ConfigVersion, len(resp.Peers), len(resp.ACL), len(resp.Routes))
			lastConfig = resp.ConfigVersion
		}
	}
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	cp := *addr
	return &cp
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
