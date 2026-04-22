// Package forward 在打通的 KCP 隧道上做 TCP 端口转发。
//
// 一条隧道可同时承载多个 TCP 连接（依赖 smux 多路复用）。
// 每端既是入口（listen 本地 TCP，把流量送到对端）也是出口（收对端流量，dial 本地目标）。
//
// 流内协议（应用层握手，非常简单）：
//
//	发起方 OpenStream 后第一件事：写 "<host>:<port>\n"
//	接收方 AcceptStream 后第一件事：读上述一行，再写 "OK\n" 或 "ERR <reason>\n"
//	之后双向透传字节流
package forward

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/xtaci/smux"
)

// Rule 表示一条转发规则。
// 本机 LocalPort 上接入的 TCP 连接 -> 经隧道 -> 对端 dial Target。
type Rule struct {
	LocalPort int
	Target    string // host:port
	Name      string // 日志用
}

// ParseRule 解析 "LOCAL:HOST:PORT" 格式的字符串，例如 "13389:127.0.0.1:3389"。
func ParseRule(s string) (Rule, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return Rule{}, fmt.Errorf("格式应为 LOCAL:HOST:PORT, 实际: %q", s)
	}
	lp, err := strconv.Atoi(parts[0])
	if err != nil || lp <= 0 || lp >= 65536 {
		return Rule{}, fmt.Errorf("非法本地端口: %q", parts[0])
	}
	target := parts[1]
	if _, _, err := net.SplitHostPort(target); err != nil {
		return Rule{}, fmt.Errorf("非法目标 host:port %q: %w", target, err)
	}
	return Rule{LocalPort: lp, Target: target, Name: fmt.Sprintf("%d->%s", lp, target)}, nil
}

const maxTargetLine = 256
const streamHandshakeTimeout = 20 * time.Second

// RunIngress 为每条转发规则起一个 TCP 监听，每个入站 TCP 连接走一条 smux 流。
func RunIngress(sess *smux.Session, rules []Rule) error {
	for _, r := range rules {
		r := r
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", r.LocalPort))
		if err != nil {
			return fmt.Errorf("listen %d: %w", r.LocalPort, err)
		}
		log.Printf("[forward] ingress %s listening on 127.0.0.1:%d", r.Name, r.LocalPort)
		go func() {
			<-sess.CloseChan()
			_ = ln.Close()
		}()
		go acceptLocalTCP(ln, sess, r)
	}
	return nil
}

func acceptLocalTCP(ln net.Listener, sess *smux.Session, r Rule) {
	for {
		tc, err := ln.Accept()
		if err != nil {
			log.Printf("[forward] %s accept stopped: %v", r.Name, err)
			return
		}
		go handleIngressConn(tc, sess, r)
	}
}

func handleIngressConn(tc net.Conn, sess *smux.Session, r Rule) {
	defer tc.Close()
	stream, err := sess.OpenStream()
	if err != nil {
		log.Printf("[forward] %s open stream failed: %v", r.Name, err)
		return
	}
	defer stream.Close()

	// 写首行请求目标
	if _, err := fmt.Fprintf(stream, "%s\n", r.Target); err != nil {
		log.Printf("[forward] %s write target failed: %v", r.Name, err)
		return
	}

	// 读对端响应
	stream.SetReadDeadline(time.Now().Add(streamHandshakeTimeout))
	br := bufio.NewReader(stream)
	resp, err := br.ReadString('\n')
	if err != nil {
		log.Printf("[forward] %s no response from remote target handshake after %s: %v", r.Name, streamHandshakeTimeout, err)
		return
	}
	stream.SetReadDeadline(time.Time{})
	resp = strings.TrimRight(resp, "\r\n")
	if resp != "OK" {
		log.Printf("[forward] %s remote rejected: %s", r.Name, resp)
		return
	}

	// 双向透传
	log.Printf("[forward] %s stream opened (%s <-> tunnel)", r.Name, tc.RemoteAddr())
	bridge(tc, stream, br)
	log.Printf("[forward] %s stream closed", r.Name)
}

// RunEgress 持续 Accept 对端发来的流，按首行指定的目标 dial 本地 TCP。
func RunEgress(sess *smux.Session) {
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			log.Printf("[forward] egress accept stopped: %v", err)
			return
		}
		go handleEgressStream(stream)
	}
}

func handleEgressStream(stream *smux.Stream) {
	defer stream.Close()

	stream.SetReadDeadline(time.Now().Add(streamHandshakeTimeout))
	br := bufio.NewReader(io.LimitReader(stream, maxTargetLine))
	line, err := br.ReadString('\n')
	if err != nil {
		log.Printf("[forward/egress] read target failed after %s: %v", streamHandshakeTimeout, err)
		return
	}
	stream.SetReadDeadline(time.Time{})
	target := strings.TrimRight(line, "\r\n")

	// dial 本地目标
	tc, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		log.Printf("[forward/egress] dial %s failed: %v", target, err)
		fmt.Fprintf(stream, "ERR %s\n", err.Error())
		return
	}
	defer tc.Close()

	if _, err := stream.Write([]byte("OK\n")); err != nil {
		log.Printf("[forward/egress] write OK to %s failed: %v", target, err)
		return
	}

	log.Printf("[forward/egress] %s <-> %s opened", target, stream.RemoteAddr())
	bridge(tc, stream, br)
	log.Printf("[forward/egress] %s closed", target)
}

// bridge 在 TCP 连接和 smux stream 之间做双向数据拷贝。
// br 里可能已经缓冲了 stream 的部分数据（读首行时的剩余），需要优先消费。
func bridge(tc net.Conn, stream *smux.Stream, br *bufio.Reader) {
	done := make(chan struct{}, 2)
	go func() {
		// tunnel -> local tcp：先把缓冲里的残留字节刷出去
		if br != nil && br.Buffered() > 0 {
			io.Copy(tc, io.LimitReader(br, int64(br.Buffered())))
		}
		io.Copy(tc, stream)
		if halfCloser, ok := tc.(interface{ CloseWrite() error }); ok {
			halfCloser.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		io.Copy(stream, tc)
		stream.Close()
		done <- struct{}{}
	}()
	<-done
	<-done
}

// ErrNoRules 当命令行未配置转发规则且未显式允许空配置时返回
var ErrNoRules = errors.New("no forward rules")
