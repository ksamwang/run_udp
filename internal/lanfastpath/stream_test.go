package lanfastpath

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"udp_tunnel_demo/internal/store"
)

func TestHeaderRoundTrip(t *testing.T) {
	header, err := NewHeader(store.ProfileBulk, "172.16.10.3", 445)
	if err != nil {
		t.Fatal(err)
	}
	if header.Target != "172.16.10.3:445" || header.Profile != store.ProfileBulk {
		t.Fatalf("bad header: %+v", header)
	}
	got, err := ParseHeaderLine(header.Line())
	if err != nil {
		t.Fatal(err)
	}
	if got != header {
		t.Fatalf("roundtrip mismatch: got=%+v want=%+v", got, header)
	}
}

func TestHeaderRejectsBadTarget(t *testing.T) {
	if _, err := NewHeader(store.ProfileBulk, "", 445); err == nil {
		t.Fatal("empty host should fail")
	}
	if _, err := NewHeader(store.ProfileBulk, "172.16.10.3", 70000); err == nil {
		t.Fatal("bad port should fail")
	}
	if _, err := ParseHeaderLine("bulk\tbad-target\n"); err == nil {
		t.Fatal("bad target should fail")
	}
}

func TestReadHeaderPreservesBufferedPayload(t *testing.T) {
	header, br, err := ReadHeader(strings.NewReader("bulk\t127.0.0.1:445\npayload"))
	if err != nil {
		t.Fatal(err)
	}
	if header.Profile != store.ProfileBulk || header.Target != "127.0.0.1:445" {
		t.Fatalf("bad header: %+v", header)
	}
	rest, err := io.ReadAll(br)
	if err != nil {
		t.Fatal(err)
	}
	if string(rest) != "payload" {
		t.Fatalf("buffered payload lost: %q", rest)
	}
}

func TestBridgeCopiesBufferedAndLiveBytes(t *testing.T) {
	tcp := newMemoryConn("")
	stream := newMemoryStream("from-stream")
	br := bufio.NewReader(strings.NewReader("prefetched"))
	if _, err := br.Peek(1); err != nil {
		t.Fatal(err)
	}
	done := make(chan BridgeStats, 1)
	go func() {
		done <- Bridge(tcp, stream, br)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not stop")
	}
	if got := tcp.writes.String(); got != "prefetchedfrom-stream" {
		t.Fatalf("bad stream->tcp copy: %q", got)
	}
}

func readExact(t *testing.T, r io.Reader, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

type memoryConn struct {
	reads  *bytes.Reader
	writes bytes.Buffer
}

func newMemoryConn(read string) *memoryConn {
	return &memoryConn{reads: bytes.NewReader([]byte(read))}
}

func (c *memoryConn) Read(b []byte) (int, error)       { return c.reads.Read(b) }
func (c *memoryConn) Write(b []byte) (int, error)      { return c.writes.Write(b) }
func (c *memoryConn) Close() error                     { return nil }
func (c *memoryConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (c *memoryConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (c *memoryConn) SetDeadline(time.Time) error      { return nil }
func (c *memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (c *memoryConn) SetWriteDeadline(time.Time) error { return nil }

type memoryStream struct {
	*memoryConn
}

func newMemoryStream(read string) *memoryStream {
	return &memoryStream{memoryConn: newMemoryConn(read)}
}

type dummyAddr string

func (a dummyAddr) Network() string { return string(a) }
func (a dummyAddr) String() string  { return string(a) }
