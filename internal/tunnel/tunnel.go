// Package tunnel 在打通的 UDP 通道之上提供可靠、有序的字节流（基于 KCP）。
//
// 设计要点：
//   - 客户端已有一个 *net.UDPConn 和打洞得到的对端地址
//   - 本包提供一个 PacketConn 适配器：外观上是 net.PacketConn（KCP 需要），
//     实际所有写都转发到学到的对端地址，所有读都从 Feed 喂进来的队列里取
//   - Demuxer 负责区分流量：JSON 协议包 vs KCP 包。KCP 包会 Feed 到本适配器
//   - 一方 Listener（等待 Accept），一方 Dialer（主动 NewConn）
package tunnel

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	kcp "github.com/xtaci/kcp-go/v5"
)

// IsProtocolJSON 判断一个 UDP 载荷是不是我们自己的 JSON 协议包。
// 规则：JSON 对象以 '{' 开头（ASCII 0x7B）；KCP 包头第一个字节是 conv ID 低字节，
// 我们使用的 conv ID 低字节不会是 0x7B。
func IsProtocolJSON(b []byte) bool {
	return len(b) > 0 && b[0] == '{'
}

// PacketConn 适配 KCP 需要的 net.PacketConn，但底层复用外部已有的 UDP socket。
type PacketConn struct {
	real       *net.UDPConn
	peer       *atomic.Pointer[net.UDPAddr] // 实际对端地址（可能被动态更新）
	inbox      chan inbound
	closed     chan struct{}
	closedOnce sync.Once
	rdead      atomic.Pointer[time.Time]
}

type inbound struct {
	data []byte
	src  *net.UDPAddr
}

func NewPacketConn(real *net.UDPConn, peer *atomic.Pointer[net.UDPAddr]) *PacketConn {
	return &PacketConn{
		real:   real,
		peer:   peer,
		inbox:  make(chan inbound, 512),
		closed: make(chan struct{}),
	}
}

// Feed 由外部 demuxer 在收到 KCP 数据包时调用。
func (p *PacketConn) Feed(data []byte, src *net.UDPAddr) {
	cp := make([]byte, len(data))
	copy(cp, data)
	select {
	case p.inbox <- inbound{data: cp, src: src}:
	case <-p.closed:
	default:
		// 缓冲满则丢弃，KCP 会重传
	}
}

func (p *PacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	var timerC <-chan time.Time
	if tp := p.rdead.Load(); tp != nil && !tp.IsZero() {
		d := time.Until(*tp)
		if d <= 0 {
			return 0, nil, errTimeout{}
		}
		t := time.NewTimer(d)
		defer t.Stop()
		timerC = t.C
	}
	select {
	case <-p.closed:
		return 0, nil, net.ErrClosed
	case <-timerC:
		return 0, nil, errTimeout{}
	case x := <-p.inbox:
		n := copy(b, x.data)
		return n, x.src, nil
	}
}

// WriteTo 忽略 addr 参数，总是写入当前已学习到的对端地址。
// KCP 内部传给我们的 addr 是我们在 ReadFrom 返回的那个，
// 但打洞场景下对端地址可能动态变化，以 peer 指针为准更稳妥。
func (p *PacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	dst := p.peer.Load()
	if dst == nil {
		return 0, errors.New("no peer address")
	}
	return p.real.WriteToUDP(b, dst)
}

func (p *PacketConn) Close() error {
	p.closedOnce.Do(func() { close(p.closed) })
	return nil
}

func (p *PacketConn) LocalAddr() net.Addr                { return p.real.LocalAddr() }
func (p *PacketConn) SetDeadline(t time.Time) error      { p.rdead.Store(&t); return nil }
func (p *PacketConn) SetReadDeadline(t time.Time) error  { p.rdead.Store(&t); return nil }
func (p *PacketConn) SetWriteDeadline(_ time.Time) error { return nil }

type errTimeout struct{}

func (errTimeout) Error() string   { return "i/o timeout" }
func (errTimeout) Timeout() bool   { return true }
func (errTimeout) Temporary() bool { return true }

// Open 在已打通的 PacketConn 上建立 KCP 会话。
//
//	listener=true:  等待对端先发起（kcp.ServeConn + Accept）
//	listener=false: 主动发起（kcp.NewConn2）
//
// 两端约定由 ID 字典序小的一方当 listener，另一方当 dialer。
func Open(pc *PacketConn, listener bool, convID uint32) (net.Conn, error) {
	if listener {
		lis, err := kcp.ServeConn(nil, 0, 0, pc)
		if err != nil {
			return nil, fmt.Errorf("kcp serve: %w", err)
		}
		// 设 Accept 超时，避免永远卡住
		lis.SetReadDeadline(time.Now().Add(30 * time.Second))
		sess, err := lis.AcceptKCP()
		if err != nil {
			return nil, fmt.Errorf("kcp accept: %w", err)
		}
		tuneSession(sess)
		return sess, nil
	}

	dst := pc.peer.Load()
	if dst == nil {
		return nil, errors.New("peer address unknown")
	}
	sess, err := kcp.NewConn2(dst, nil, 0, 0, pc)
	if err != nil {
		return nil, fmt.Errorf("kcp newconn: %w", err)
	}
	_ = convID
	tuneSession(sess)
	// 立即发一个 ping 探测包：KCP 本身懒启动，必须有 Write 才会发首包，
	// 对端的 Accept 才会返回。Listener 端会识别并丢弃这个探测包。
	if _, err := sess.Write([]byte(handshakeMarker)); err != nil {
		sess.Close()
		return nil, fmt.Errorf("kcp handshake send: %w", err)
	}
	return sess, nil
}

// handshakeMarker 是 Dialer 建立会话后立即写入的首个数据，
// 用于触发 Listener 侧 Accept 返回。Listener 读到后需要丢弃。
const handshakeMarker = "\x00KCP-HELLO\n"

// ConsumeHandshake 消费 Listener 端收到的首个探测包，确认隧道双向就绪。
// 如果收到的首个字节流不是预期的 marker，会报错。
func ConsumeHandshake(sess net.Conn) error {
	sess.SetReadDeadline(time.Now().Add(30 * time.Second))
	defer sess.SetReadDeadline(time.Time{})
	buf := make([]byte, len(handshakeMarker))
	// 用 Read 而不是 ReadFull，因为 KCP 可能一次给出超过 marker 的数据；
	// 但我们在 Dialer 首 Write 只发 marker，后续 Write 由用户触发，因此大概率一次读出
	n, err := sess.Read(buf)
	if err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	if string(buf[:n]) != handshakeMarker {
		return fmt.Errorf("unexpected handshake: %q", string(buf[:n]))
	}
	return nil
}

func tuneSession(s *kcp.UDPSession) {
	// 极速模式：无延迟、10ms 间隔、快速重传、关拥塞控制
	s.SetNoDelay(1, 10, 2, 1)
	s.SetWindowSize(512, 512)
	s.SetMtu(1200) // 留足空间避免被 NAT 设备分片丢弃
	s.SetStreamMode(true)
}
