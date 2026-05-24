package lanfastpath

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"udp_tunnel_demo/internal/store"
)

const (
	MaxHeaderLine          = 256
	StreamHandshakeTimeout = 20 * time.Second
	copyBufferSize         = 256 * 1024
)

type Header struct {
	Profile string
	Target  string
}

type BridgeStats struct {
	ToTCP    int64
	ToStream int64
}

func NewHeader(profile, host string, port int) (Header, error) {
	profile = store.NormalizeProfile(profile)
	if profile == "" {
		profile = store.ProfileBulk
	}
	if strings.TrimSpace(host) == "" || port <= 0 || port > 65535 {
		return Header{}, errors.New("bad fast path target")
	}
	return Header{Profile: profile, Target: net.JoinHostPort(strings.TrimSpace(host), strconv.Itoa(port))}, nil
}

func (h Header) Line() string {
	return h.Profile + "\t" + h.Target + "\n"
}

func ParseHeaderLine(line string) (Header, error) {
	line = strings.TrimRight(line, "\r\n")
	profile, target, ok := strings.Cut(line, "\t")
	if !ok {
		return Header{}, errors.New("bad fast path header")
	}
	profile = store.NormalizeProfile(profile)
	if profile == "" {
		profile = store.ProfileBulk
	}
	if _, _, err := net.SplitHostPort(target); err != nil {
		return Header{}, fmt.Errorf("bad fast path target: %w", err)
	}
	return Header{Profile: profile, Target: target}, nil
}

func ReadHeader(r io.Reader) (Header, *bufio.Reader, error) {
	br := bufio.NewReader(io.LimitReader(r, MaxHeaderLine))
	line, err := br.ReadString('\n')
	if err != nil {
		return Header{}, br, err
	}
	header, err := ParseHeaderLine(line)
	return header, br, err
}

func Bridge(tcp net.Conn, stream io.ReadWriteCloser, br *bufio.Reader) BridgeStats {
	done := make(chan int64, 2)
	go func() {
		var n int64
		if br != nil && br.Buffered() > 0 {
			copied, _ := io.Copy(tcp, io.LimitReader(br, int64(br.Buffered())))
			n += copied
		}
		copied, _ := copyWithBuffer(tcp, stream)
		n += copied
		if halfCloser, ok := tcp.(interface{ CloseWrite() error }); ok {
			_ = halfCloser.CloseWrite()
		} else {
			_ = tcp.Close()
		}
		done <- n
	}()
	go func() {
		n, _ := copyWithBuffer(stream, tcp)
		_ = stream.Close()
		done <- n
	}()
	return BridgeStats{ToTCP: <-done, ToStream: <-done}
}

func copyWithBuffer(dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, copyBufferSize)
	return io.CopyBuffer(dst, src, buf)
}

func TuneTCPConn(conn net.Conn, profile string) {
	if store.NormalizeProfile(profile) != store.ProfileBulk {
		return
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tcp.SetReadBuffer(4 * 1024 * 1024)
	_ = tcp.SetWriteBuffer(4 * 1024 * 1024)
}
