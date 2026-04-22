// UDP 打洞客户端（阶段 2：打通后建立 KCP 可靠隧道）。
//
// 流程：
//  1. 向 Rendezvous Server register
//  2. 拿到对端公网地址，周期性 punch
//  3. 任一方向通路打通后，按 ID 字典序决定角色：
//     较小的一方当 KCP Listener（被动 Accept），较大的一方当 Dialer（主动发起）
//  4. 建立 KCP session 后，stdin 行 -> session.Write；session.Read -> stdout
//     底层仍共用同一个 UDP socket：demuxer 把 JSON 协议包和 KCP 包分流
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/xtaci/smux"

	"udp_tunnel_demo/internal/forward"
	"udp_tunnel_demo/internal/protocol"
	"udp_tunnel_demo/internal/tunnel"
	"udp_tunnel_demo/internal/upnp"
)

// multiFlag 支持 -forward a -forward b
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	serverAddr := flag.String("server", "", "Rendezvous server address, e.g. 1.2.3.4:7000")
	id := flag.String("id", "", "my ID")
	peerID := flag.String("peer", "", "peer ID to connect")
	probe := flag.Bool("probe", false, "NAT 类型探测模式（不参与打洞，只诊断）")
	altPort := flag.Int("alt-port", 7002, "服务端 STUN 备用端口")
	punchTimeout := flag.Duration("punch-timeout", 15*time.Second, "打洞超时时间, 超时后自动切换到 TURN 中继")
	forceRelay := flag.Bool("force-relay", false, "跳过打洞直接走 TURN 中继")
	noUpnp := flag.Bool("no-upnp", false, "禁用 UPnP 主动端口映射")
	upnpTimeout := flag.Duration("upnp-timeout", 4*time.Second, "UPnP 发现/映射超时时间")
	var forwards multiFlag
	flag.Var(&forwards, "forward", "TCP 转发规则, 格式 LOCAL_PORT:HOST:PORT (可重复, 例如 -forward 13389:127.0.0.1:3389)")
	flag.Parse()

	if *serverAddr == "" {
		log.Fatal("usage: -server <ip:port> [-probe] | -id <me> -peer <other>")
	}
	if *probe {
		runProbe(*serverAddr, *altPort)
		return
	}
	if *id == "" || *peerID == "" {
		log.Fatal("usage: -server <ip:port> -id <me> -peer <other> [-forward LOCAL:HOST:PORT ...]")
	}

	var rules []forward.Rule
	for _, s := range forwards {
		r, err := forward.ParseRule(s)
		if err != nil {
			log.Fatalf("bad -forward %q: %v", s, err)
		}
		rules = append(rules, r)
	}

	srvAddr, err := net.ResolveUDPAddr("udp", *serverAddr)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Printf("[%s] local socket: %s", *id, conn.LocalAddr())

	// 尝试 UPnP 主动端口映射（失败不影响后续流程）
	var upnpMapping *upnp.Mapping
	var upnpAddrStr string
	if !*noUpnp {
		localPort := conn.LocalAddr().(*net.UDPAddr).Port
		ctx, cancel := context.WithTimeout(context.Background(), *upnpTimeout+time.Second)
		m, uerr := upnp.Try(ctx, localPort, fmt.Sprintf("udp-tunnel %s", *id), *upnpTimeout)
		cancel()
		if uerr != nil {
			log.Printf("[%s] UPnP 映射失败（忽略）: %v", *id, uerr)
		} else {
			upnpMapping = m
			upnpAddrStr = m.External()
			log.Printf("[%s] 🗺️  UPnP 映射成功: %s -> :%d", *id, upnpAddrStr, m.InternalPort)
			defer func() {
				if err := upnpMapping.Close(); err != nil {
					log.Printf("[%s] UPnP 清除映射失败: %v", *id, err)
				} else {
					log.Printf("[%s] UPnP 映射已清除", *id)
				}
			}()
		}
	}

	reg := &protocol.Message{Type: protocol.MsgRegister, From: *id, Peer: *peerID, UpnpAddr: upnpAddrStr}
	b, _ := protocol.Encode(reg)
	if _, err := conn.WriteToUDP(b, srvAddr); err != nil {
		log.Fatal(err)
	}
	log.Printf("[%s] registered to server, waiting for peer info...", *id)

	var peerAddr atomic.Pointer[net.UDPAddr]
	var peerUpnpAddr atomic.Pointer[net.UDPAddr] // 对端声明的 UPnP 公网地址（附加打洞目标）
	var punched atomic.Bool
	var punchStarted atomic.Bool
	var isRelay atomic.Bool // 进入 TURN 中继模式后为 true；此后不再改 peerAddr
	var rxCount atomic.Uint64
	punchedCh := make(chan struct{})
	var closeOnce struct{ done atomic.Bool }

	// 进入中继：把 peerAddr 锁定为服务器地址，之后所有 KCP 写出都经服务器转发
	enterRelay := func(reason string) {
		if isRelay.CompareAndSwap(false, true) {
			log.Printf("[%s] 🔁 进入 TURN 中继（%s），peerAddr -> %s", *id, reason, srvAddr)
			peerAddr.Store(srvAddr)
			punched.Store(true)
			if closeOnce.done.CompareAndSwap(false, true) {
				close(punchedCh)
			}
		}
	}

	pc := tunnel.NewPacketConn(conn, &peerAddr)

	// 读协程：JSON 协议包自己处理，其他（KCP）喂给 PacketConn
	go func() {
		buf := make([]byte, 4096)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				log.Printf("read error: %v", err)
				return
			}
			data := buf[:n]
			if !tunnel.IsProtocolJSON(data) {
				pc.Feed(data, src)
				continue
			}
			msg, err := protocol.Decode(data)
			if err != nil {
				continue
			}
			switch msg.Type {
			case protocol.MsgPeerInfo:
				if isRelay.Load() {
					// 已经进入中继，忽略服务器推来的直连地址
					continue
				}
				paddr, err := net.ResolveUDPAddr("udp", msg.Addr)
				if err != nil {
					log.Printf("bad peer addr: %v", err)
					continue
				}
				peerAddr.Store(paddr)
				log.Printf("[%s] got peer %s address: %s", *id, msg.Peer, paddr)
				if msg.UpnpAddr != "" {
					if uaddr, uerr := net.ResolveUDPAddr("udp", msg.UpnpAddr); uerr == nil {
						peerUpnpAddr.Store(uaddr)
						log.Printf("[%s] got peer %s UPnP address: %s", *id, msg.Peer, uaddr)
					} else {
						log.Printf("[%s] bad peer upnp addr %q: %v", *id, msg.UpnpAddr, uerr)
					}
				}
				if punchStarted.CompareAndSwap(false, true) {
					log.Printf("[%s] start punching -> %s (upnp=%v)", *id, paddr, peerUpnpAddr.Load())
					go punchLoop(conn, &peerAddr, &peerUpnpAddr, &punched, *id)
				}

			case protocol.MsgPunch:
				rxCount.Add(1)
				adoptPeerAddrSafe(&peerAddr, src, *id, &isRelay)
				ack := &protocol.Message{Type: protocol.MsgPunchAck, From: *id}
				bb, _ := protocol.Encode(ack)
				conn.WriteToUDP(bb, src)
				if punched.CompareAndSwap(false, true) {
					log.Printf("[%s] ✅ hole punched! peer=%s (via incoming punch)", *id, src)
					if closeOnce.done.CompareAndSwap(false, true) {
						close(punchedCh)
					}
				}

			case protocol.MsgPunchAck:
				rxCount.Add(1)
				adoptPeerAddrSafe(&peerAddr, src, *id, &isRelay)
				if punched.CompareAndSwap(false, true) {
					log.Printf("[%s] ✅ hole punched! peer=%s (got ack)", *id, src)
					if closeOnce.done.CompareAndSwap(false, true) {
						close(punchedCh)
					}
				}

			case protocol.MsgKeepAlive:
				// 静默忽略
			}
		}
	}()

	// 保活协程：维持 NAT 映射
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for range t.C {
			if !punched.Load() {
				continue
			}
			paddr := peerAddr.Load()
			if paddr == nil {
				continue
			}
			ka := &protocol.Message{Type: protocol.MsgKeepAlive, From: *id}
			data, _ := protocol.Encode(ka)
			conn.WriteToUDP(data, paddr)
		}
	}()

	// 强制中继：跳过打洞直接走服务器转发
	if *forceRelay {
		enterRelay("-force-relay")
	}

	// 打洞超时自动兜底
	go func() {
		t := time.NewTimer(*punchTimeout)
		defer t.Stop()
		select {
		case <-punchedCh:
			return
		case <-t.C:
			enterRelay(fmt.Sprintf("打洞 %s 未成功", *punchTimeout))
		}
	}()

	log.Printf("[%s] waiting for hole punch (timeout=%s)...", *id, *punchTimeout)
	<-punchedCh

	// 判断当前走的是直连还是中继（看最终 peerAddr 是不是服务器地址）
	if pa := peerAddr.Load(); pa != nil && pa.String() == srvAddr.String() {
		log.Printf("[%s] 🔁 数据路径：TURN 中继（经服务器转发）", *id)
	} else {
		log.Printf("[%s] 🎯 数据路径：P2P 直连", *id)
	}

	// 决定角色：ID 小的当 Listener，大的当 Dialer
	isListener := *id < *peerID
	role := "dialer"
	if isListener {
		role = "listener"
	}
	log.Printf("[%s] opening KCP tunnel as %s...", *id, role)

	// 稍等一下，确保对端也已打通
	time.Sleep(300 * time.Millisecond)

	kcpConn, err := tunnel.Open(pc, isListener, 1)
	if err != nil {
		log.Fatalf("[%s] open tunnel failed: %v", *id, err)
	}
	defer kcpConn.Close()
	if isListener {
		if err := tunnel.ConsumeHandshake(kcpConn); err != nil {
			log.Fatalf("[%s] handshake failed: %v", *id, err)
		}
	}
	log.Printf("[%s] 🔒 KCP tunnel established (role=%s)", *id, role)

	// 在 KCP 之上跑 smux，提供多路复用的 TCP 流抽象
	smuxCfg := smux.DefaultConfig()
	smuxCfg.Version = 2
	smuxCfg.KeepAliveInterval = 10 * time.Second
	smuxCfg.KeepAliveTimeout = 30 * time.Second

	var mux *smux.Session
	if isListener {
		mux, err = smux.Server(kcpConn, smuxCfg)
	} else {
		mux, err = smux.Client(kcpConn, smuxCfg)
	}
	if err != nil {
		log.Fatalf("[%s] smux setup failed: %v", *id, err)
	}
	defer mux.Close()
	log.Printf("[%s] 📦 smux session ready, accepting forwarded streams", *id)

	// 出口：接收对端 OpenStream 发起的转发请求
	go forward.RunEgress(mux)

	// 入口：按本地规则监听 TCP 端口
	if len(rules) > 0 {
		if err := forward.RunIngress(mux, rules); err != nil {
			log.Fatalf("[%s] ingress setup failed: %v", *id, err)
		}
	} else {
		log.Printf("[%s] 无本地转发规则（仅作为出口端接收对端请求）", *id)
	}

	// 阻塞直到隧道关闭或收到信号
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-mux.CloseChan():
		log.Printf("[%s] smux session closed by peer", *id)
	case <-stop:
		log.Printf("[%s] signal received, shutting down", *id)
	}
}

// adoptPeerAddr 在收到对端包时更新我方记录的对端地址。
// 真实 NAT 打洞里，服务器告知的"对端地址"是对端↔服务器这对流量的映射，
// 未必等于对端↔我方这对流量的映射（地址依赖映射的 NAT 会分配不同端口）。
// 真正能走通的地址就是我们实际收到包的源地址，以后回包就用它。
func adoptPeerAddr(peerAddr *atomic.Pointer[net.UDPAddr], src *net.UDPAddr, id string) {
	cur := peerAddr.Load()
	if cur == nil || cur.String() != src.String() {
		cp := *src
		peerAddr.Store(&cp)
		if cur != nil {
			log.Printf("[%s] peer addr learned from packet: %s -> %s", id, cur, src)
		}
	}
}

// adoptPeerAddrSafe 仅在未进入中继时才接受 peer 地址更新。
func adoptPeerAddrSafe(peerAddr *atomic.Pointer[net.UDPAddr], src *net.UDPAddr, id string, isRelay *atomic.Bool) {
	if isRelay.Load() {
		return
	}
	adoptPeerAddr(peerAddr, src, id)
}

// NAT 类型探测：见服务器 -stun-alt 端口
func runProbe(server string, altPort int) {
	host, _, err := net.SplitHostPort(server)
	if err != nil {
		log.Fatalf("bad server: %v", err)
	}
	primary, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		log.Fatal(err)
	}
	alt, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, altPort))
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Printf("probe local socket: %s", conn.LocalAddr())

	ask := func(target *net.UDPAddr, label string) string {
		req := &protocol.Message{Type: protocol.MsgStunReq}
		b, _ := protocol.Encode(req)
		if _, err := conn.WriteToUDP(b, target); err != nil {
			log.Fatalf("send to %s: %v", target, err)
		}
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 4096)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				log.Fatalf("no reply from %s (%s): %v", target, label, err)
			}
			if src.String() != target.String() {
				continue
			}
			msg, err := protocol.Decode(buf[:n])
			if err != nil || msg.Type != protocol.MsgStunResp {
				continue
			}
			log.Printf("%s: server %s sees me as %s", label, target, msg.Addr)
			return msg.Addr
		}
	}

	a1 := ask(primary, "probe#1")
	a2 := ask(alt, "probe#2")

	_, p1, _ := net.SplitHostPort(a1)
	_, p2, _ := net.SplitHostPort(a2)

	fmt.Println()
	fmt.Println("==========  NAT 探测结果  ==========")
	fmt.Printf("主端口观察到:  %s\n", a1)
	fmt.Printf("备端口观察到:  %s\n", a2)
	if p1 == p2 {
		fmt.Println("类型判定:     Cone 类 NAT（Full / Restricted / Port-Restricted）")
		fmt.Println("打洞前景:     ✅ 能打通（和同类或 Full Cone 对端）")
	} else {
		fmt.Println("类型判定:     ❌ Symmetric NAT（对称型）")
		fmt.Println("打洞前景:     标准打洞失败；需要端口预测或 TURN 中继")
	}
	fmt.Println("====================================")
}

// 持续向对端候选地址发 punch 包，直到打通。
// peerAddr 是服务器观察到的地址；peerUpnpAddr 是对端 UPnP 主动映射地址（可为 nil）。
// 两个都有就都发，哪条路先通就用哪条（adoptPeerAddr 会锁定）。
func punchLoop(conn *net.UDPConn, peerAddr, peerUpnpAddr *atomic.Pointer[net.UDPAddr], punched *atomic.Bool, id string) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	attempts := 0
	punchMsg := &protocol.Message{Type: protocol.MsgPunch, From: id}
	data, _ := protocol.Encode(punchMsg)
	for range t.C {
		if punched.Load() {
			return
		}
		paddr := peerAddr.Load()
		if paddr == nil {
			return
		}
		uaddr := peerUpnpAddr.Load()
		if _, err := conn.WriteToUDP(data, paddr); err != nil {
			log.Printf("punch send error: %v", err)
		}
		if uaddr != nil && uaddr.String() != paddr.String() {
			if _, err := conn.WriteToUDP(data, uaddr); err != nil {
				log.Printf("punch send (upnp) error: %v", err)
			}
		}
		attempts++
		if attempts%4 == 0 {
			if uaddr != nil {
				log.Printf("[%s] punching... sent=%d target=%s + upnp=%s", id, attempts, paddr, uaddr)
			} else {
				log.Printf("[%s] punching... sent=%d target=%s", id, attempts, paddr)
			}
		}
		if attempts > 120 {
			log.Printf("[%s] ❌ punch failed after %d attempts", id, attempts)
			return
		}
	}
}
