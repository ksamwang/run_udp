package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"udp_tunnel_demo/internal/lan"
	"udp_tunnel_demo/internal/lantransport"
	"udp_tunnel_demo/internal/packet"
	"udp_tunnel_demo/internal/protocol"
	"udp_tunnel_demo/internal/secure"
	"udp_tunnel_demo/internal/store"
	"udp_tunnel_demo/internal/tunnel"
	"udp_tunnel_demo/internal/upnp"
	"udp_tunnel_demo/internal/wintun"

	"golang.org/x/crypto/curve25519"
)

const (
	lanPacketBatchSize    = 16
	lanPacketPollInterval = 10 * time.Millisecond
	lanConfigRefreshEvery = 10 * time.Second
	lanUPnPTimeout        = 4 * time.Second
	lanPunchTimeout       = 30 * time.Second
	lanTunnelRetryDelay   = 3 * time.Second
	lanRelayRotateAfter   = 3
	lanRelayMaxAge        = 60 * time.Second
	lanRotationCooldown   = 60 * time.Second
	lanKCPFrameMax        = 64 * 1024
	lanKCPReadyTimeout    = 35 * time.Second
	lanRelayBackoff       = 500 * time.Millisecond
	lanPendingTTL         = 5 * time.Second
	lanPendingMaxFrames   = 64
	lanPendingMaxBytes    = 512 * 1024
	lanDatagramReadyFrame = "UDPTunnelLAN-DATAGRAM-READY"
)

var lanKCPReadyFrame = []byte("\x00LAN-KCP-READY\n")

var tryLANUPnPFunc = tryLANUPnP

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
	go pollRelayPackets(ctx, serverHTTP, adapter, router, link, p2p, deviceID)
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
			if isWintunNoPacketError(err) {
				continue
			}
			log.Printf("LAN packet read failed: %v", err)
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
	pending := newLANPendingQueue(lanPendingMaxFrames, lanPendingMaxBytes, lanPendingTTL)
	var relayBackoffUntil time.Time
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	flush := func() {
		if len(batch) > 0 {
			pending.Add(batch)
			batch = batch[:0]
		}
		frames := pending.Frames()
		if len(frames) == 0 {
			return
		}
		delivered := map[int]bool{}
		relay := make([]packet.RoutedFrame, 0, len(frames))
		for i, frame := range frames {
			if p2p != nil && p2p.Send(frame) == nil {
				_, _ = link.Send(frame.DstDevice, frame.Payload)
				delivered[i] = true
				continue
			}
			if p2p == nil || !p2p.CanRelay(frame.DstDevice) {
				continue
			}
			relay = append(relay, frame)
		}
		if len(relay) == 0 {
			pending.Remove(delivered)
			return
		}
		if strings.TrimSpace(serverHTTP) == "" {
			for _, frame := range relay {
				log.Printf("LAN tunnel unavailable; keep pending dst=%s bytes=%d", frame.DstDevice, len(frame.Payload))
			}
			pending.Remove(delivered)
			return
		}
		if !relayBackoffUntil.IsZero() && time.Now().Before(relayBackoffUntil) {
			pending.Remove(delivered)
			return
		}
		relayFrames, err := p2p.SealRelayFrames(relay)
		if err != nil {
			log.Printf("LAN relay seal failed: %v", err)
			relayBackoffUntil = time.Now().Add(lanRelayBackoff)
			pending.Remove(delivered)
			return
		}
		if len(relayFrames) == 0 {
			pending.Remove(delivered)
			return
		}
		if err := postRelayFrames(ctx, serverHTTP, relayFrames); err != nil {
			log.Printf("LAN relay send failed: %v", err)
			relayBackoffUntil = time.Now().Add(lanRelayBackoff)
			pending.Remove(delivered)
			return
		}
		for i := range frames {
			delivered[i] = true
		}
		pending.Remove(delivered)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-outbound:
			batch = append(batch, frame)
			flush()
			if len(batch) > 0 || pending.Len() > 0 {
				timer.Reset(20 * time.Millisecond)
			}
		case <-timer.C:
			flush()
			if pending.Len() > 0 {
				timer.Reset(20 * time.Millisecond)
			}
		}
	}
}

type lanPendingFrame struct {
	frame packet.RoutedFrame
	at    time.Time
	size  int
}

type lanPendingQueue struct {
	maxFrames int
	maxBytes  int
	ttl       time.Duration
	bytes     int
	frames    []lanPendingFrame
}

func newLANPendingQueue(maxFrames, maxBytes int, ttl time.Duration) *lanPendingQueue {
	return &lanPendingQueue{maxFrames: maxFrames, maxBytes: maxBytes, ttl: ttl}
}

func (q *lanPendingQueue) Add(frames []packet.RoutedFrame) {
	now := time.Now()
	q.prune(now)
	for _, frame := range frames {
		size := len(frame.Payload)
		q.frames = append(q.frames, lanPendingFrame{frame: frame, at: now, size: size})
		q.bytes += size
		q.trim()
	}
}

func (q *lanPendingQueue) Frames() []packet.RoutedFrame {
	q.prune(time.Now())
	out := make([]packet.RoutedFrame, 0, len(q.frames))
	for _, item := range q.frames {
		out = append(out, item.frame)
	}
	return out
}

func (q *lanPendingQueue) Remove(delivered map[int]bool) {
	if len(delivered) == 0 {
		q.prune(time.Now())
		return
	}
	next := q.frames[:0]
	q.bytes = 0
	for i, item := range q.frames {
		if delivered[i] {
			continue
		}
		next = append(next, item)
		q.bytes += item.size
	}
	q.frames = next
	q.prune(time.Now())
}

func (q *lanPendingQueue) Len() int {
	q.prune(time.Now())
	return len(q.frames)
}

func (q *lanPendingQueue) Bytes() int {
	q.prune(time.Now())
	return q.bytes
}

func (q *lanPendingQueue) prune(now time.Time) {
	if q == nil || q.ttl <= 0 {
		return
	}
	next := q.frames[:0]
	q.bytes = 0
	for _, item := range q.frames {
		if now.Sub(item.at) > q.ttl {
			continue
		}
		next = append(next, item)
		q.bytes += item.size
	}
	q.frames = next
}

func (q *lanPendingQueue) trim() {
	for len(q.frames) > q.maxFrames || (q.maxBytes > 0 && q.bytes > q.maxBytes) {
		q.bytes -= q.frames[0].size
		copy(q.frames, q.frames[1:])
		q.frames = q.frames[:len(q.frames)-1]
	}
}

type lanP2P struct {
	conn         *net.UDPConn
	connMu       sync.RWMutex
	upnpMapping  *upnp.Mapping
	peers        map[string]*lanP2PPeer
	peerMu       sync.RWMutex
	peerConv     map[uint32]string
	server       *net.UDPAddr
	deviceID     string
	identity     lan.Identity
	adapter      *wintun.Adapter
	router       *packet.Router
	link         *packet.LinkManager
	x25519Priv   [32]byte
	x25519Pub    string
	upnpAddr     string
	registering  map[string]bool
	relayTimers  map[string]bool
	openRetries  map[string]bool
	readLoopGen  atomic.Uint64
	rotating     atomic.Bool
	lastRotation atomic.Int64
}

type lanP2PPeer struct {
	id            string
	publicKey     string
	x25519Pub     string
	addr          atomic.Pointer[net.UDPAddr]
	upnpAddr      atomic.Pointer[net.UDPAddr]
	punched       atomic.Bool
	punching      atomic.Bool
	datagramReady atomic.Bool
	connected     atomic.Bool
	isRelay       atomic.Bool
	registers     atomic.Uint64
	openFailures  atomic.Uint64
	relaySince    atomic.Int64
	tx            *packet.Codec
	rx            *packet.Codec
	pc            *tunnel.PacketConn
	kcp           net.Conn
	kcpMu         sync.Mutex
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
	upnpMapping, upnpAddr := tryLANUPnPFunc(ctx, conn, deviceID)
	p := &lanP2P{
		conn: conn, upnpMapping: upnpMapping, server: server, deviceID: deviceID, identity: identity, adapter: adapter, router: router, link: link, x25519Priv: priv, x25519Pub: pub,
		upnpAddr: upnpAddr, peers: map[string]*lanP2PPeer{}, peerConv: map[uint32]string{}, registering: map[string]bool{}, relayTimers: map[string]bool{}, openRetries: map[string]bool{},
	}
	go func() {
		<-ctx.Done()
		p.closeCurrentSocket()
	}()
	if upnpMapping != nil {
		go p.refreshCurrentUPnPMapping(ctx)
	}
	p.startReadLoop(ctx)
	p.UpsertPeers(ctx, identity, resp.Peers)
	log.Printf("LAN P2P started: socket=%s server=%s upnp=%q peers=%d", conn.LocalAddr(), server, upnpAddr, len(resp.Peers))
	return p
}

func (p *lanP2P) currentConn() *net.UDPConn {
	p.connMu.RLock()
	defer p.connMu.RUnlock()
	return p.conn
}

func (p *lanP2P) currentSocketAddr() string {
	conn := p.currentConn()
	if conn == nil || conn.LocalAddr() == nil {
		return "-"
	}
	return conn.LocalAddr().String()
}

func (p *lanP2P) closeCurrentSocket() {
	p.connMu.Lock()
	conn := p.conn
	mapping := p.upnpMapping
	p.conn = nil
	p.upnpMapping = nil
	p.upnpAddr = ""
	p.connMu.Unlock()
	if mapping != nil {
		_ = mapping.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
}

func (p *lanP2P) startReadLoop(ctx context.Context) {
	conn := p.currentConn()
	if conn == nil {
		return
	}
	gen := p.readLoopGen.Add(1)
	go p.readLoop(ctx, conn, gen, p.adapter, p.router, p.link)
}

func (p *lanP2P) UpsertPeers(ctx context.Context, identity lan.Identity, peers []lanBootstrapPeer) {
	if p == nil {
		return
	}
	p.peerMu.Lock()
	if p.peers == nil {
		p.peers = map[string]*lanP2PPeer{}
	}
	if p.peerConv == nil {
		p.peerConv = map[uint32]string{}
	}
	if p.registering == nil {
		p.registering = map[string]bool{}
	}
	if p.relayTimers == nil {
		p.relayTimers = map[string]bool{}
	}
	if p.openRetries == nil {
		p.openRetries = map[string]bool{}
	}
	p.peerMu.Unlock()
	for _, peer := range peers {
		peerID := strings.TrimSpace(peer.DeviceID)
		if peerID == "" || peerID == p.deviceID {
			continue
		}
		shouldRegister := false
		p.peerMu.Lock()
		if existing := p.peers[peerID]; existing == nil {
			p.peers[peerID] = &lanP2PPeer{id: peerID, publicKey: peer.PublicKey}
			p.peerConv[secure.ConvID("", p.deviceID, peerID, store.ProfileLANPacket)] = peerID
			shouldRegister = true
		} else {
			existing.publicKey = peer.PublicKey
			p.peerConv[secure.ConvID("", p.deviceID, peerID, store.ProfileLANPacket)] = peerID
			if !p.registering[peerID] {
				shouldRegister = true
			}
		}
		if shouldRegister {
			p.registering[peerID] = true
		}
		if !p.relayTimers[peerID] {
			p.relayTimers[peerID] = true
			go p.relayFallbackTimer(ctx, peerID)
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
	if peer == nil {
		return packet.ErrLinkUnavailable
	}
	if peer.datagramReady.Load() {
		addr := peer.addr.Load()
		if err := lantransport.Send(p.currentConn(), addr, peer.tx, frame.Payload); err == nil {
			return nil
		} else {
			log.Printf("LAN P2P datagram send failed: peer=%s err=%v", peer.id, err)
		}
	}
	if !peer.connected.Load() {
		return packet.ErrLinkUnavailable
	}
	peer.kcpMu.Lock()
	defer peer.kcpMu.Unlock()
	if peer.kcp == nil {
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
	return writeLANFrame(peer.kcp, payload)
}

func (p *lanP2P) useDatagramFastPath(peer *lanP2PPeer, link *packet.LinkManager) bool {
	if peer == nil || peer.isRelay.Load() || !peer.datagramReady.Load() || peer.tx == nil || peer.rx == nil {
		return false
	}
	addr := peer.addr.Load()
	if addr == nil {
		return false
	}
	if link != nil {
		_, _ = link.UpsertPeer(packet.PeerEndpoint{DeviceID: peer.id, Addr: addr.String()}, packet.PeerEndpoint{Addr: "udp-relay"}, true)
	}
	log.Printf("LAN P2P datagram path ready: peer=%s addr=%s path=p2p_datagram", peer.id, addr)
	return true
}

func (p *lanP2P) openTunnelUnlessDatagramReady(ctx context.Context, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager, peer *lanP2PPeer) {
	if p.useDatagramFastPath(peer, link) {
		return
	}
	go p.openTunnel(ctx, adapter, router, link, peer)
}

func (p *lanP2P) CanRelay(peerID string) bool {
	peer := p.peer(peerID)
	return peer != nil && peer.tx != nil && peer.rx != nil
}

func (p *lanP2P) SendDatagram(frame packet.RoutedFrame) error {
	peer := p.peer(frame.DstDevice)
	if peer == nil || !peer.datagramReady.Load() {
		return packet.ErrLinkUnavailable
	}
	return lantransport.Send(p.currentConn(), peer.addr.Load(), peer.tx, frame.Payload)
}

func (p *lanP2P) SealRelayFrames(frames []packet.RoutedFrame) ([]packet.RoutedFrame, error) {
	out := make([]packet.RoutedFrame, 0, len(frames))
	for _, frame := range frames {
		peer := p.peer(frame.DstDevice)
		if peer == nil || peer.tx == nil {
			continue
		}
		sealed, err := peer.tx.Seal(frame.Payload)
		if err != nil {
			return nil, err
		}
		next := frame
		next.Payload = sealed
		out = append(out, next)
	}
	return out, nil
}

func (p *lanP2P) OpenRelayFrame(frame packet.RoutedFrame) (packet.RoutedFrame, error) {
	peer := p.peer(frame.SrcDevice)
	if peer == nil || peer.rx == nil {
		return frame, packet.ErrPeerNotFound
	}
	plain, err := peer.rx.Open(frame.Payload)
	if err != nil {
		return frame, err
	}
	frame.Payload = plain
	return frame, nil
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
	p.connMu.RLock()
	upnpAddr := p.upnpAddr
	p.connMu.RUnlock()
	msg := &protocol.Message{
		Type: protocol.MsgLANRegister, From: p.deviceID, Peer: peerID, Profile: profile,
		UpnpAddr: upnpAddr, Payload: p.x25519Pub, Timestamp: ts, Signature: sig,
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
			msg := &protocol.Message{Type: protocol.MsgPunch, From: p.deviceID, Profile: store.ProfileLANPacket, Payload: lanDatagramReadyFrame}
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
	conn := p.currentConn()
	if conn == nil {
		log.Printf("LAN P2P control send skipped: socket unavailable dst=%s", dst)
		return
	}
	b, _ := protocol.Encode(msg)
	if _, err := conn.WriteToUDP(b, dst); err != nil {
		log.Printf("LAN P2P control send failed: dst=%s err=%v", dst, err)
	}
}

func (p *lanP2P) relayFallbackTimer(ctx context.Context, peerID string) {
	t := time.NewTimer(lanPunchTimeout)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return
	case <-t.C:
	}
	peer := p.peer(peerID)
	p.peerMu.Lock()
	if p.relayTimers != nil {
		p.relayTimers[peerID] = false
	}
	p.peerMu.Unlock()
	if peer == nil || peer.punched.Load() {
		return
	}
	peer.addr.Store(cloneUDPAddr(p.server))
	peer.isRelay.Store(true)
	peer.relaySince.Store(time.Now().UnixNano())
	if peer.pc == nil {
		peer.pc = p.newPeerPacketConn(&peer.addr)
	}
	peer.punched.Store(true)
	log.Printf("LAN P2P relay mode: peer=%s reason=punch timeout %s", peer.id, lanPunchTimeout)
	go p.openTunnel(ctx, p.adapter, p.router, p.link, peer)
}

func (p *lanP2P) resetRelayTimer(ctx context.Context, peerID string) {
	p.peerMu.Lock()
	if p.relayTimers == nil {
		p.relayTimers = map[string]bool{}
	}
	if p.relayTimers[peerID] {
		p.peerMu.Unlock()
		return
	}
	p.relayTimers[peerID] = true
	p.peerMu.Unlock()
	go p.relayFallbackTimer(ctx, peerID)
}

func (p *lanP2P) scheduleOpenRetry(ctx context.Context, peer *lanP2PPeer) {
	if p == nil || peer == nil {
		return
	}
	p.peerMu.Lock()
	if p.openRetries == nil {
		p.openRetries = map[string]bool{}
	}
	if p.openRetries[peer.id] {
		p.peerMu.Unlock()
		return
	}
	p.openRetries[peer.id] = true
	p.peerMu.Unlock()
	go func(peerID string) {
		timer := time.NewTimer(lanTunnelRetryDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		p.peerMu.Lock()
		if p.openRetries != nil {
			p.openRetries[peerID] = false
		}
		p.peerMu.Unlock()
		peer := p.peer(peerID)
		if peer == nil || peer.connected.Load() || !peer.punched.Load() {
			return
		}
		if p.useDatagramFastPath(peer, p.link) {
			return
		}
		log.Printf("LAN P2P retrying KCP tunnel: peer=%s relay=%v", peer.id, peer.isRelay.Load())
		go p.openTunnel(ctx, p.adapter, p.router, p.link, peer)
	}(peer.id)
}

func (p *lanP2P) maybeRotateSocketAfterRelayFailure(ctx context.Context, peer *lanP2PPeer, reason string) bool {
	if p == nil || peer == nil || !peer.isRelay.Load() {
		return false
	}
	failures := peer.openFailures.Add(1)
	relaySinceRaw := peer.relaySince.Load()
	relayAge := time.Duration(0)
	if relaySinceRaw > 0 {
		relayAge = time.Since(time.Unix(0, relaySinceRaw))
	}
	if failures < lanRelayRotateAfter && relayAge < lanRelayMaxAge {
		return false
	}
	if p.hasReadyPeer(peer.id) {
		log.Printf("LAN P2P socket rotation skipped: reason=%s peer=%s failures=%d ready_peer_exists=true", reason, peer.id, failures)
		return false
	}
	go p.rotateSocketAndRestartPunch(ctx, fmt.Sprintf("%s peer=%s failures=%d relay_age=%s", reason, peer.id, failures, relayAge.Round(time.Second)))
	return true
}

func (p *lanP2P) hasReadyPeer(except string) bool {
	p.peerMu.RLock()
	defer p.peerMu.RUnlock()
	for id, peer := range p.peers {
		if id == except || peer == nil {
			continue
		}
		if peer.connected.Load() {
			peer.kcpMu.Lock()
			ready := peer.kcp != nil
			peer.kcpMu.Unlock()
			if ready {
				return true
			}
		}
	}
	return false
}

func (p *lanP2P) rotateSocketAndRestartPunch(ctx context.Context, reason string) {
	if p == nil {
		return
	}
	if !p.rotating.CompareAndSwap(false, true) {
		return
	}
	defer p.rotating.Store(false)
	now := time.Now()
	last := time.Unix(0, p.lastRotation.Load())
	if !last.IsZero() && now.Sub(last) < lanRotationCooldown {
		log.Printf("LAN P2P socket rotation skipped: reason=%s cooldown_remaining=%s", reason, (lanRotationCooldown - now.Sub(last)).Round(time.Second))
		return
	}
	oldAddr := p.currentSocketAddr()
	log.Printf("LAN P2P rotating UDP socket: reason=%s old=%s", reason, oldAddr)
	newConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Printf("LAN P2P socket rotation failed: listen udp: %v", err)
		return
	}
	newMapping, newUPnP := tryLANUPnPFunc(ctx, newConn, p.deviceID)

	p.connMu.Lock()
	oldConn := p.conn
	oldMapping := p.upnpMapping
	p.conn = newConn
	p.upnpMapping = newMapping
	p.upnpAddr = newUPnP
	p.lastRotation.Store(now.UnixNano())
	p.connMu.Unlock()

	if oldMapping != nil {
		_ = oldMapping.Close()
	}
	if oldConn != nil {
		_ = oldConn.Close()
	}
	p.resetAllPeersAfterSocketRotation(ctx)
	p.startReadLoop(ctx)
	if newMapping != nil {
		go p.refreshCurrentUPnPMapping(ctx)
	}
	log.Printf("LAN P2P socket rotated: new=%s upnp=%q", newConn.LocalAddr(), newUPnP)
	p.reregisterAllPeers(ctx)
}

func (p *lanP2P) resetAllPeersAfterSocketRotation(ctx context.Context) {
	p.peerMu.RLock()
	peers := make([]*lanP2PPeer, 0, len(p.peers))
	for _, peer := range p.peers {
		peers = append(peers, peer)
	}
	p.peerMu.RUnlock()
	p.peerMu.Lock()
	p.relayTimers = map[string]bool{}
	p.openRetries = map[string]bool{}
	p.peerMu.Unlock()
	for _, peer := range peers {
		if peer == nil {
			continue
		}
		peer.kcpMu.Lock()
		if peer.kcp != nil {
			_ = peer.kcp.Close()
			peer.kcp = nil
		}
		if peer.pc != nil {
			_ = peer.pc.Close()
			peer.pc = nil
		}
		peer.connected.Store(false)
		peer.punched.Store(false)
		peer.punching.Store(false)
		peer.isRelay.Store(false)
		peer.datagramReady.Store(false)
		peer.openFailures.Store(0)
		peer.relaySince.Store(0)
		peer.addr.Store(nil)
		peer.upnpAddr.Store(nil)
		peer.kcpMu.Unlock()
		log.Printf("LAN P2P peer reset after socket rotation: peer=%s", peer.id)
		p.resetRelayTimer(ctx, peer.id)
	}
}

func (p *lanP2P) reregisterAllPeers(ctx context.Context) {
	p.peerMu.RLock()
	ids := make([]string, 0, len(p.peers))
	for id := range p.peers {
		ids = append(ids, id)
	}
	p.peerMu.RUnlock()
	log.Printf("LAN P2P re-registering peers after socket rotation: peers=%d", len(ids))
	for _, id := range ids {
		p.writeLANRegister(p.identity, id)
	}
}

func (p *lanP2P) resetPeerAfterOpenFailure(ctx context.Context, peer *lanP2PPeer, kcpConn net.Conn) {
	if kcpConn != nil {
		_ = kcpConn.Close()
	}
	peer.kcpMu.Lock()
	if peer.kcp == kcpConn {
		peer.kcp = nil
	}
	peer.connected.Store(false)
	relay := peer.isRelay.Load()
	if peer.pc != nil {
		_ = peer.pc.Close()
		peer.pc = nil
	}
	if relay {
		peer.addr.Store(cloneUDPAddr(p.server))
		peer.pc = p.newPeerPacketConn(&peer.addr)
		peer.punched.Store(true)
		peer.punching.Store(false)
	} else {
		peer.punched.Store(false)
		peer.punching.Store(false)
		peer.datagramReady.Store(false)
		p.resetRelayTimer(ctx, peer.id)
	}
	peer.kcpMu.Unlock()
	if relay {
		if !p.maybeRotateSocketAfterRelayFailure(ctx, peer, "relay_open_failed") {
			p.scheduleOpenRetry(ctx, peer)
		}
	}
}

func (p *lanP2P) readLoop(ctx context.Context, conn *net.UDPConn, gen uint64, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager) {
	buf := make([]byte, 65535)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() == nil {
				if p.readLoopGen.Load() == gen {
					log.Printf("LAN P2P read stopped: %v", err)
				}
			}
			return
		}
		data := append([]byte(nil), buf[:n]...)
		if len(data) > 0 && data[0] == '{' {
			p.handleControl(ctx, data, src, adapter, router, link)
			continue
		}
		peerID := ""
		if lantransport.IsFrame(data) {
			peerID = p.peerIDByAddr(src)
		} else {
			peerID = p.peerIDByPacket(data, src)
		}
		if peerID == "" {
			if src.String() == p.server.String() {
				log.Printf("LAN P2P ignored non-json control from server: bytes=%d", len(data))
			}
			continue
		}
		peer := p.peer(peerID)
		if peer == nil || peer.pc == nil {
			if peer != nil && lantransport.IsFrame(data) {
				p.handleDatagramFrame(adapter, router, link, peer, data)
			}
			continue
		}
		if lantransport.IsFrame(data) {
			p.handleDatagramFrame(adapter, router, link, peer, data)
			continue
		}
		peer.pc.Feed(data, src)
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

func (p *lanP2P) refreshCurrentUPnPMapping(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		p.connMu.RLock()
		mapping := p.upnpMapping
		p.connMu.RUnlock()
		if mapping == nil {
			return
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := mapping.Refresh(refreshCtx)
		cancel()
		if err != nil {
			log.Printf("LAN P2P UPnP refresh failed: device=%s err=%v", p.deviceID, err)
			continue
		}
		log.Printf("LAN P2P UPnP mapping refreshed: device=%s external=%s", p.deviceID, mapping.External())
	}
}

func (p *lanP2P) newPeerPacketConn(peerAddr *atomic.Pointer[net.UDPAddr]) *tunnel.PacketConn {
	conn := p.currentConn()
	if conn == nil {
		return nil
	}
	return tunnel.NewPacketConn(conn, peerAddr)
}

func (p *lanP2P) openTunnel(ctx context.Context, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager, peer *lanP2PPeer) {
	if p.useDatagramFastPath(peer, link) {
		return
	}
	if peer == nil || !peer.connected.CompareAndSwap(false, true) {
		return
	}
	if adapter == nil || router == nil || link == nil {
		peer.connected.Store(false)
		return
	}
	if peer.pc == nil {
		peer.pc = p.newPeerPacketConn(&peer.addr)
	}
	isListener := p.deviceID < peer.id
	role := "dialer"
	if isListener {
		role = "listener"
	}
	convID := secure.ConvID("", p.deviceID, peer.id, store.ProfileLANPacket)
	log.Printf("LAN P2P opening KCP tunnel: peer=%s role=%s conv=%d", peer.id, role, convID)
	kcpConn, err := tunnel.Open(peer.pc, isListener, convID, store.ProfileLANPacket)
	if err != nil {
		p.resetPeerAfterOpenFailure(ctx, peer, nil)
		log.Printf("LAN P2P KCP open failed: peer=%s err=%v", peer.id, err)
		return
	}
	if err := confirmLANKCPReady(kcpConn, isListener); err != nil {
		p.resetPeerAfterOpenFailure(ctx, peer, kcpConn)
		log.Printf("LAN P2P KCP ready check failed: peer=%s role=%s err=%v", peer.id, role, err)
		return
	}
	peer.kcpMu.Lock()
	peer.kcp = kcpConn
	peer.kcpMu.Unlock()
	peer.openFailures.Store(0)
	peer.relaySince.Store(0)
	readyPath := !peer.isRelay.Load()
	_, _ = link.UpsertPeer(packet.PeerEndpoint{DeviceID: peer.id, Addr: valueOrDash(peerAddrString(&peer.addr))}, packet.PeerEndpoint{Addr: "udp-relay"}, readyPath)
	path := packet.LinkPathP2P
	if peer.isRelay.Load() {
		path = packet.LinkPathRelay
	}
	log.Printf("LAN P2P KCP tunnel ready: peer=%s role=%s path=%s", peer.id, role, path)
	go func() {
		<-ctx.Done()
		_ = kcpConn.Close()
	}()
	go p.readTunnelFrames(ctx, adapter, router, link, peer, kcpConn)
}

func confirmLANKCPReady(conn net.Conn, listener bool) error {
	if listener {
		if err := tunnel.ConsumeHandshake(conn); err != nil {
			return err
		}
		if err := writeLANFrame(conn, lanKCPReadyFrame); err != nil {
			return fmt.Errorf("send ready ack: %w", err)
		}
		return nil
	}
	return waitLANKCPReadyAck(conn)
}

func waitLANKCPReadyAck(conn net.Conn) error {
	if err := conn.SetReadDeadline(time.Now().Add(lanKCPReadyTimeout)); err != nil {
		return err
	}
	defer conn.SetReadDeadline(time.Time{})
	payload, err := readLANFrame(conn)
	if err != nil {
		return fmt.Errorf("read ready ack: %w", err)
	}
	if !bytes.Equal(payload, lanKCPReadyFrame) {
		return fmt.Errorf("unexpected ready ack: %q", string(payload))
	}
	return nil
}

func (p *lanP2P) readTunnelFrames(ctx context.Context, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager, peer *lanP2PPeer, conn net.Conn) {
	for {
		payload, err := readLANFrame(conn)
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) {
				log.Printf("LAN P2P KCP read failed: peer=%s err=%v", peer.id, err)
			}
			peer.kcpMu.Lock()
			if peer.kcp == conn {
				peer.kcp = nil
				peer.connected.Store(false)
				peer.punched.Store(false)
				peer.punching.Store(false)
				peer.isRelay.Store(false)
				p.resetRelayTimer(ctx, peer.id)
			}
			peer.kcpMu.Unlock()
			_ = conn.Close()
			return
		}
		if peer.rx != nil {
			plain, err := peer.rx.Open(payload)
			if err != nil {
				log.Printf("LAN P2P packet decrypt failed: peer=%s err=%v", peer.id, err)
				continue
			}
			payload = plain
		}
		router.RecordInbound(packet.RoutedFrame{SrcDevice: peer.id, DstDevice: p.deviceID, Payload: payload, PacketType: packet.TypeIPv4})
		path := packet.LinkPathP2P
		if peer.isRelay.Load() {
			path = packet.LinkPathRelay
		}
		_ = link.Receive(peer.id, payload, path)
		if err := adapter.WritePacket(payload); err != nil {
			log.Printf("LAN P2P write packet failed: peer=%s bytes=%d err=%v", peer.id, len(payload), err)
		}
	}
}

func (p *lanP2P) handleDatagramFrame(adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager, peer *lanP2PPeer, data []byte) {
	if peer == nil || peer.rx == nil || adapter == nil || router == nil || link == nil {
		return
	}
	payload, err := lantransport.Open(peer.rx, data)
	if err != nil {
		log.Printf("LAN P2P datagram decrypt failed: peer=%s err=%v", peer.id, err)
		return
	}
	router.RecordInbound(packet.RoutedFrame{SrcDevice: peer.id, DstDevice: p.deviceID, Payload: payload, PacketType: packet.TypeIPv4})
	_ = link.Receive(peer.id, payload, packet.LinkPathP2P)
	if err := adapter.WritePacket(payload); err != nil {
		log.Printf("LAN P2P datagram write packet failed: peer=%s bytes=%d err=%v", peer.id, len(payload), err)
	}
}

func writeLANFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > lanKCPFrameMax {
		return fmt.Errorf("bad LAN frame size %d", len(payload))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readLANFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > lanKCPFrameMax {
		return nil, fmt.Errorf("bad LAN frame size %d", n)
	}
	payload := make([]byte, int(n))
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func peerAddrString(ptr *atomic.Pointer[net.UDPAddr]) string {
	if ptr == nil {
		return ""
	}
	addr := ptr.Load()
	if addr == nil {
		return ""
	}
	return addr.String()
}

func (p *lanP2P) handleControl(ctx context.Context, data []byte, src *net.UDPAddr, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager) {
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
		relayActive := peer.isRelay.Load()
		peer.addr.Store(addr)
		if peer.pc == nil && !relayActive {
			peer.pc = p.newPeerPacketConn(&peer.addr)
		}
		if strings.TrimSpace(msg.UpnpAddr) != "" {
			upnpAddr, err := net.ResolveUDPAddr("udp", msg.UpnpAddr)
			if err != nil {
				log.Printf("LAN P2P bad peer upnp addr: peer=%s addr=%q err=%v", msg.Peer, msg.UpnpAddr, err)
			} else {
				peer.upnpAddr.Store(upnpAddr)
			}
		}
		if peer.tx != nil && peer.rx != nil {
			_, _ = link.UpsertPeer(packet.PeerEndpoint{DeviceID: peer.id, Addr: addr.String()}, packet.PeerEndpoint{Addr: "udp-relay"}, true)
		}
		log.Printf("LAN P2P peer info: peer=%s addr=%s upnp=%q codec_ready=%v", peer.id, addr, msg.UpnpAddr, peer.tx != nil && peer.rx != nil)
		p.startPunchLoop(ctx, peer)
		if !relayActive && peer.punched.Load() && !peer.connected.Load() {
			p.openTunnelUnlessDatagramReady(ctx, adapter, router, link, peer)
		}
	case protocol.MsgPunch:
		if store.NormalizeProfile(msg.Profile) != store.ProfileLANPacket {
			return
		}
		peer := p.peer(msg.From)
		if peer == nil {
			log.Printf("LAN P2P punch ignored: unknown peer=%s addr=%s", msg.From, src)
			return
		}
		if !peer.isRelay.Load() {
			peer.addr.Store(cloneUDPAddr(src))
		}
		if msg.Payload == lanDatagramReadyFrame && peer.tx != nil && peer.rx != nil {
			peer.datagramReady.Store(true)
		}
		if peer.punched.CompareAndSwap(false, true) {
			log.Printf("LAN P2P punched via incoming punch: peer=%s addr=%s", peer.id, src)
			p.openTunnelUnlessDatagramReady(ctx, adapter, router, link, peer)
		}
		_, _ = link.UpsertPeer(packet.PeerEndpoint{DeviceID: peer.id, Addr: src.String()}, packet.PeerEndpoint{Addr: "udp-relay"}, true)
		p.writeControl(src, &protocol.Message{Type: protocol.MsgPunchAck, From: p.deviceID, Profile: store.ProfileLANPacket, Payload: lanDatagramReadyFrame})
	case protocol.MsgPunchAck:
		if store.NormalizeProfile(msg.Profile) != store.ProfileLANPacket {
			return
		}
		peer := p.peer(msg.From)
		if peer == nil {
			log.Printf("LAN P2P punch ack ignored: unknown peer=%s addr=%s", msg.From, src)
			return
		}
		if !peer.isRelay.Load() {
			peer.addr.Store(cloneUDPAddr(src))
		}
		if msg.Payload == lanDatagramReadyFrame && peer.tx != nil && peer.rx != nil {
			peer.datagramReady.Store(true)
		}
		if peer.punched.CompareAndSwap(false, true) {
			log.Printf("LAN P2P punched via ack: peer=%s addr=%s", peer.id, src)
			p.openTunnelUnlessDatagramReady(ctx, adapter, router, link, peer)
		}
		_, _ = link.UpsertPeer(packet.PeerEndpoint{DeviceID: peer.id, Addr: src.String()}, packet.PeerEndpoint{Addr: "udp-relay"}, true)
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

func (p *lanP2P) peerIDByPacket(data []byte, addr *net.UDPAddr) string {
	if id := p.peerIDByConv(data); id != "" {
		return id
	}
	return p.peerIDByAddr(addr)
}

func (p *lanP2P) peerIDByConv(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	conv := binary.LittleEndian.Uint32(data[:4])
	p.peerMu.RLock()
	defer p.peerMu.RUnlock()
	return p.peerConv[conv]
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

func pollRelayPackets(ctx context.Context, serverHTTP string, adapter *wintun.Adapter, router *packet.Router, link *packet.LinkManager, p2p *lanP2P, deviceID string) {
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
			if p2p != nil {
				plain, err := p2p.OpenRelayFrame(routed)
				if err != nil {
					log.Printf("LAN relay frame decrypt failed: src=%s err=%v", frame.SrcDevice, err)
					continue
				}
				routed = plain
				payload = plain.Payload
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
