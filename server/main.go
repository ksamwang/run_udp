// Rendezvous Server：帮助两个 NAT 后的 Client 交换公网地址。
//
// 工作流程：
//  1. Client A 发送 register{from:"A", peer:"B"} 到服务器
//  2. 服务器记下 A 的公网地址（UDP 源地址就是 NAT 映射后的公网地址）
//  3. Client B 发送 register{from:"B", peer:"A"}，服务器记下 B 的地址
//  4. 一旦 A、B 都到齐，服务器把对方的公网地址分别发给双方
//  5. 之后 A、B 开始互相打洞，服务器使命结束
//
// 同时提供一个 HTTP 健康检查接口，方便用浏览器确认服务存活。
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"udp_tunnel_demo/internal/protocol"
)

type peer struct {
	id       string
	addr     *net.UDPAddr
	upnpAddr string // 对端自称的 UPnP 主动映射公网地址（可选）
	want     string
	lastSeen time.Time
}

var (
	startTime     = time.Now()
	totalRegister atomic.Uint64
	totalPaired   atomic.Uint64
	totalRelayed  atomic.Uint64 // 中继的字节数（TURN 模式）

	mu    sync.Mutex
	peers = map[string]*peer{} // id -> peer

	// 地址级配对表：src.String() -> 对端 UDP 地址。
	// 任何非 JSON 控制包从已配对源发来时，直接转发给对端（TURN 中继）。
	pairs sync.Map
)

func main() {
	udpAddr := flag.String("listen", ":7000", "UDP 监听地址（主：Rendezvous + STUN 主端口）")
	stunAlt := flag.String("stun-alt", ":7002", "UDP 备用端口（用于 NAT 类型探测的第二个观察点）")
	httpAddr := flag.String("http", ":7001", "HTTP 健康检查监听地址")
	flag.Parse()

	go runHTTP(*httpAddr)
	go runStunAlt(*stunAlt)
	runUDP(*udpAddr)
}

// 备用端口：纯 STUN 回显。客户端从同一 socket 把请求发到这个端口，
// 如果收到的 "observed address" 端口和主端口回的不一样 => 对称 NAT。
func runStunAlt(addr string) {
	uaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", uaddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Printf("rendezvous server (STUN-ALT) listening on %s", addr)

	buf := make([]byte, 4096)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("stun-alt read error: %v", err)
			continue
		}
		msg, err := protocol.Decode(buf[:n])
		if err != nil || msg.Type != protocol.MsgStunReq {
			continue
		}
		resp := &protocol.Message{Type: protocol.MsgStunResp, Addr: src.String()}
		b, _ := protocol.Encode(resp)
		conn.WriteToUDP(b, src)
	}
}

func runUDP(addr string) {
	uaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", uaddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Printf("rendezvous server (UDP) listening on %s", addr)

	buf := make([]byte, 4096)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("read error: %v", err)
			continue
		}
		data := buf[:n]

		// 非 JSON 包 -> TURN 中继：如果源在配对表里，直接转发给对端
		if len(data) == 0 || data[0] != '{' {
			if dstRaw, ok := pairs.Load(src.String()); ok {
				dst := dstRaw.(*net.UDPAddr)
				if _, err := conn.WriteToUDP(data, dst); err == nil {
					totalRelayed.Add(uint64(n))
				}
			}
			continue
		}

		msg, err := protocol.Decode(data)
		if err != nil {
			log.Printf("bad packet from %s: %v", src, err)
			continue
		}
		if msg.Type == protocol.MsgStunReq {
			resp := &protocol.Message{Type: protocol.MsgStunResp, Addr: src.String()}
			b, _ := protocol.Encode(resp)
			conn.WriteToUDP(b, src)
			continue
		}
		if msg.Type != protocol.MsgRegister {
			continue
		}

		log.Printf("register: id=%s want=%s from=%s upnp=%q", msg.From, msg.Peer, src, msg.UpnpAddr)
		totalRegister.Add(1)

		mu.Lock()
		if old, ok := peers[msg.From]; ok && old.addr.String() != src.String() {
			log.Printf("  (re-register: %s moved from %s to %s)", msg.From, old.addr, src)
			pairs.Delete(old.addr.String())
		}
		peers[msg.From] = &peer{id: msg.From, addr: src, upnpAddr: msg.UpnpAddr, want: msg.Peer, lastSeen: time.Now()}

		self := peers[msg.From]
		other, ok := peers[msg.Peer]
		if ok && other.want == msg.From {
			sendPeer(conn, self, other)
			sendPeer(conn, other, self)
			// 登记 TURN 中继配对
			pairs.Store(self.addr.String(), other.addr)
			pairs.Store(other.addr.String(), self.addr)
			log.Printf("paired: %s(%s) <-> %s(%s)", self.id, self.addr, other.id, other.addr)
			totalPaired.Add(1)
		} else {
			log.Printf("  waiting for peer %s to register...", msg.Peer)
		}
		mu.Unlock()
	}
}

func sendPeer(conn *net.UDPConn, to, about *peer) {
	msg := &protocol.Message{
		Type:     protocol.MsgPeerInfo,
		Peer:     about.id,
		Addr:     about.addr.String(),
		UpnpAddr: about.upnpAddr,
	}
	b, _ := protocol.Encode(msg)
	if _, err := conn.WriteToUDP(b, to.addr); err != nil {
		log.Printf("send peer info to %s failed: %v", to.addr, err)
	}
}

func runHTTP(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/peers", handlePeers)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("udp-tunnel rendezvous server\n\n/health - 健康检查\n/peers  - 当前已注册客户端\n"))
	})
	log.Printf("rendezvous server (HTTP) listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("http listen failed: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	peerCount := len(peers)
	mu.Unlock()

	resp := map[string]any{
		"status":              "ok",
		"uptime_seconds":      int64(time.Since(startTime).Seconds()),
		"total_register":      totalRegister.Load(),
		"total_paired":        totalPaired.Load(),
		"total_relayed_bytes": totalRelayed.Load(),
		"current_peers":       peerCount,
		"server_time":         time.Now().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusOK, resp)
}

func handlePeers(w http.ResponseWriter, r *http.Request) {
	type item struct {
		ID       string `json:"id"`
		Addr     string `json:"addr"`
		UpnpAddr string `json:"upnp_addr,omitempty"`
		Want     string `json:"want"`
		LastSeen string `json:"last_seen"`
	}
	mu.Lock()
	list := make([]item, 0, len(peers))
	for _, p := range peers {
		list = append(list, item{
			ID: p.id, Addr: p.addr.String(), UpnpAddr: p.upnpAddr, Want: p.want,
			LastSeen: p.lastSeen.Format(time.RFC3339),
		})
	}
	mu.Unlock()
	writeJSON(w, http.StatusOK, list)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
