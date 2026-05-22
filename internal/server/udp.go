package server

import (
	"log"
	"net"
	"time"

	"udp_tunnel_demo/internal/protocol"
	"udp_tunnel_demo/internal/secure"
	"udp_tunnel_demo/internal/store"
)

func (a *App) runStunAlt() {
	uaddr, err := net.ResolveUDPAddr("udp", a.cfg.StunAltListen)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", uaddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Printf("rendezvous server (STUN-ALT) listening on %s", a.cfg.StunAltListen)

	buf := make([]byte, 4096)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("stun-alt read error: %v", err)
			continue
		}
		kind, payload, ok := a.openPacket(buf[:n])
		if !ok || kind != secure.KindControl {
			continue
		}
		msg, err := protocol.Decode(payload)
		if err != nil || msg.Type != protocol.MsgStunReq {
			continue
		}
		a.writeControl(conn, src, &protocol.Message{Type: protocol.MsgStunResp, Addr: src.String()})
	}
}

func (a *App) runUDP() {
	uaddr, err := net.ResolveUDPAddr("udp", a.cfg.UDPListen)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", uaddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Printf("rendezvous server (UDP) listening on %s", a.cfg.UDPListen)

	buf := make([]byte, 65535)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("read error: %v", err)
			continue
		}
		data := append([]byte(nil), buf[:n]...)
		if a.handleRelay(conn, src, data) {
			continue
		}
		kind, payload, ok := a.openPacket(data)
		if !ok || kind != secure.KindControl {
			continue
		}
		msg, err := protocol.Decode(payload)
		if err != nil {
			log.Printf("bad packet from %s: %v", src, err)
			continue
		}
		switch msg.Type {
		case protocol.MsgStunReq:
			a.writeControl(conn, src, &protocol.Message{Type: protocol.MsgStunResp, Addr: src.String()})
		case protocol.MsgRegister:
			a.handleRegister(conn, src, msg)
		}
	}
}

func (a *App) handleRelay(conn *net.UDPConn, src *net.UDPAddr, data []byte) bool {
	if !a.currentAllowRelay() {
		return false
	}
	isKCP := true
	if secure.IsFrame(data) && a.codec != nil {
		kind, _, err := a.codec.Open(data)
		if err != nil {
			return true
		}
		isKCP = kind == secure.KindKCP
	} else if len(data) > 0 && data[0] == '{' {
		isKCP = false
	}
	if !isKCP {
		return false
	}
	if dstRaw, ok := a.pairs.Load(src.String()); ok {
		route := dstRaw.(pairRoute)
		if _, err := conn.WriteToUDP(data, route.dst); err == nil {
			a.totalRelayed.Add(uint64(len(data)))
			_ = a.db.TouchSession(rctx(), route.sessionID, int64(len(data)))
			route.lastSeen = time.Now()
			a.pairs.Store(src.String(), route)
		}
		return true
	}
	return false
}

func (a *App) openPacket(data []byte) (byte, []byte, bool) {
	if secure.IsFrame(data) {
		if a.codec == nil {
			return 0, nil, false
		}
		kind, payload, err := a.codec.Open(data)
		if err != nil {
			log.Printf("secure frame open failed: %v", err)
			return 0, nil, false
		}
		return kind, payload, true
	}
	if a.currentAllowLegacy() && len(data) > 0 && data[0] == '{' {
		return secure.KindControl, data, true
	}
	return 0, nil, false
}

func (a *App) writeControl(conn *net.UDPConn, dst *net.UDPAddr, msg *protocol.Message) {
	b, _ := protocol.Encode(msg)
	if a.codec != nil {
		var err error
		b, err = a.codec.Seal(secure.KindControl, b)
		if err != nil {
			log.Printf("seal control failed: %v", err)
			return
		}
	}
	if _, err := conn.WriteToUDP(b, dst); err != nil {
		log.Printf("send control to %s failed: %v", dst, err)
	}
}

func (a *App) handleRegister(conn *net.UDPConn, src *net.UDPAddr, msg *protocol.Message) {
	profile := store.NormalizeProfile(msg.Profile)
	log.Printf("register: id=%s want=%s profile=%s from=%s upnp=%q", msg.From, msg.Peer, profile, src, msg.UpnpAddr)
	a.totalRegister.Add(1)
	name := msg.Name
	_ = a.db.UpsertDevice(rctx(), msg.From, name, src.String(), msg.UpnpAddr, msg.Peer, true)

	a.mu.Lock()
	defer a.mu.Unlock()
	byWant, ok := a.peers[msg.From]
	if !ok {
		byWant = map[string]*peer{}
		a.peers[msg.From] = byWant
	}
	slot := peerSlotKey(msg.Peer, profile)
	if old, ok := byWant[slot]; ok && old.addr.String() != src.String() {
		// 同一 (from, want) 换 socket，旧路由作废
		a.pairs.Delete(old.addr.String())
	}
	self := &peer{id: msg.From, addr: cloneUDP(src), upnpAddr: msg.UpnpAddr, want: msg.Peer, profile: profile, lastSeen: time.Now()}
	byWant[slot] = self

	other, ok := a.lookupPeer(msg.Peer, msg.From, profile)
	if !ok {
		log.Printf("  waiting for peer %s to register want=%s profile=%s...", msg.Peer, msg.From, profile)
		return
	}
	a.sendPeer(conn, self, other)
	a.sendPeer(conn, other, self)
	sessionID := a.ensurePairSession(self.id, other.id, profile, "pending")
	self.sessionID = sessionID
	other.sessionID = sessionID
	a.pairs.Store(self.addr.String(), pairRoute{dst: cloneUDP(other.addr), lastSeen: time.Now(), sessionID: sessionID})
	a.pairs.Store(other.addr.String(), pairRoute{dst: cloneUDP(self.addr), lastSeen: time.Now(), sessionID: sessionID})
	log.Printf("paired: %s(%s) <-> %s(%s) profile=%s", self.id, self.addr, other.id, other.addr, profile)
	a.totalPaired.Add(1)
}

// lookupPeer 查 (from, want) 槽。调用方需持有 a.mu。
func (a *App) lookupPeer(from, want, profile string) (*peer, bool) {
	byWant, ok := a.peers[from]
	if !ok {
		return nil, false
	}
	p, ok := byWant[peerSlotKey(want, profile)]
	return p, ok
}

func peerSlotKey(want, profile string) string {
	return want + "\x00" + store.NormalizeProfile(profile)
}

func (a *App) sendPeer(conn *net.UDPConn, to, about *peer) {
	a.writeControl(conn, to.addr, &protocol.Message{
		Type:     protocol.MsgPeerInfo,
		Peer:     about.id,
		Profile:  to.profile,
		Addr:     about.addr.String(),
		UpnpAddr: about.upnpAddr,
	})
}

func (a *App) ensurePairSession(aID, bID, profile, path string) int64 {
	key := pairKey(aID, bID, profile)
	if id, ok := a.pairByID[key]; ok {
		return id
	}
	id, err := a.db.StartSession(rctx(), aID, bID, profile, path)
	if err != nil {
		log.Printf("start session failed: %v", err)
		return 0
	}
	a.pairByID[key] = id
	return id
}

func (a *App) cleanupLoop() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		peerTTL := a.currentPeerTTL()
		pairTTL := a.currentPairTTL()
		relayIdle := a.currentRelayIdleTimeout()
		a.mu.Lock()
		for from, byWant := range a.peers {
			for want, p := range byWant {
				if now.Sub(p.lastSeen) > peerTTL {
					delete(byWant, want)
					a.pairs.Delete(p.addr.String())
				}
			}
			if len(byWant) == 0 {
				delete(a.peers, from)
			}
		}
		a.mu.Unlock()
		a.pairs.Range(func(key, value any) bool {
			route := value.(pairRoute)
			if now.Sub(route.lastSeen) > pairTTL {
				a.pairs.Delete(key)
			}
			return true
		})
		_ = a.db.MarkOfflineBefore(rctx(), now.Add(-peerTTL))
		_ = a.db.EndIdleSessions(rctx(), now.Add(-relayIdle))
	}
}
