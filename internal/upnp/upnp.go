// Package upnp 封装 UPnP IGD / NAT-PMP 端口映射，用于在家用路由器上
// 主动开一个公网 UDP 端口指向本端的隧道 socket。
//
// 成功后，对端可以直接连接 External() 返回的公网地址，
// 即使我们在对称 NAT 后面也能被连通（前提是路由器配合开 UPnP）。
//
// 失败时返回 error，调用方应当回退到普通打洞。
package upnp

import (
	"context"
	"errors"
	"fmt"
	"time"

	gonat "github.com/libp2p/go-nat"
)

// Mapping 表示一条活跃的端口映射。
type Mapping struct {
	ExternalIP   string
	ExternalPort int
	InternalPort int

	nat  gonat.NAT
	desc string
}

// External 返回 "ip:port" 形式的公网地址。
func (m *Mapping) External() string {
	return fmt.Sprintf("%s:%d", m.ExternalIP, m.ExternalPort)
}

// Close 删除端口映射。
func (m *Mapping) Close() error {
	if m == nil || m.nat == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return m.nat.DeletePortMapping(ctx, "udp", m.InternalPort)
}

// Refresh 续约映射（默认 TTL 2 小时，建议每小时调用一次）。
func (m *Mapping) Refresh(ctx context.Context) error {
	if m == nil || m.nat == nil {
		return errors.New("nil mapping")
	}
	_, err := m.nat.AddPortMapping(ctx, "udp", m.InternalPort, m.desc, 2*time.Hour)
	return err
}

// Try 尝试在指定 localPort 上建立 UDP 端口映射。
// 整个过程带 timeout 防止阻塞，失败时返回 error。
func Try(ctx context.Context, localPort int, desc string, timeout time.Duration) (*Mapping, error) {
	type result struct {
		m   *Mapping
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := gonat.DiscoverGateway(ctx)
		if err != nil {
			ch <- result{nil, fmt.Errorf("discover gateway: %w", err)}
			return
		}
		ext, err := n.GetExternalAddress()
		if err != nil {
			ch <- result{nil, fmt.Errorf("get external: %w", err)}
			return
		}
		mappedPort, err := n.AddPortMapping(ctx, "udp", localPort, desc, 2*time.Hour)
		if err != nil {
			ch <- result{nil, fmt.Errorf("add mapping: %w", err)}
			return
		}
		ch <- result{&Mapping{
			ExternalIP:   ext.String(),
			ExternalPort: mappedPort,
			InternalPort: localPort,
			nat:          n,
			desc:         desc,
		}, nil}
	}()

	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-ch:
		return r.m, r.err
	case <-t.C:
		return nil, fmt.Errorf("upnp timeout after %s", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
