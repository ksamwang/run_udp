//go:build windows

package wintun

import (
	"encoding/csv"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"

	"udp_tunnel_demo/internal/vnet"

	wintunlib "golang.zx2c4.com/wintun"
)

const tunnelType = "UDPTunnelLAN"

type windowsAdapter struct {
	adapter *wintunlib.Adapter
	session wintunlib.Session
	name    string
}

func openOrCreate(cfg Config) (*Adapter, error) {
	cfg = normalizeConfig(cfg)
	wa, err := wintunlib.OpenAdapter(cfg.Name)
	if err != nil {
		wa, err = wintunlib.CreateAdapter(cfg.Name, tunnelType, nil)
		if err != nil {
			return nil, fmt.Errorf("open/create wintun adapter: %w", err)
		}
	}
	session, err := wa.StartSession(0x400000)
	if err != nil {
		_ = wa.Close()
		return nil, fmt.Errorf("start wintun session: %w", err)
	}
	return &Adapter{impl: &windowsAdapter{adapter: wa, session: session, name: cfg.Name}}, nil
}

func (a *windowsAdapter) Close() error {
	a.session.End()
	return a.adapter.Close()
}

func (a *windowsAdapter) Configure(cfg Config) error {
	cfg = normalizeConfig(cfg)
	if cfg.Name == "" {
		cfg.Name = a.name
	}
	if cfg.IP == nil || cfg.IP.To4() == nil || strings.TrimSpace(cfg.CIDR) == "" {
		return fmt.Errorf("wintun configure requires ipv4 ip and cidr")
	}
	prefix, err := cidrPrefix(cfg.CIDR)
	if err != nil {
		return err
	}
	if err := runNetsh("interface", "ipv4", "set", "address", "name="+cfg.Name, "static", cfg.IP.String(), prefix); err != nil {
		return err
	}
	if err := runNetsh("interface", "ipv4", "set", "subinterface", cfg.Name, "mtu="+fmt.Sprint(cfg.MTU), "store=persistent"); err != nil {
		return err
	}
	if err := runNetsh("interface", "ipv4", "add", "route", cfg.CIDR, cfg.Name, cfg.IP.String(), "store=active"); err != nil && !isDuplicateRouteError(err) {
		return err
	}
	return nil
}

func (a *windowsAdapter) ReadPacket() ([]byte, error) {
	packet, err := a.session.ReceivePacket()
	if err != nil {
		return nil, err
	}
	out := append([]byte(nil), packet...)
	a.session.ReleaseReceivePacket(packet)
	return out, nil
}

func (a *windowsAdapter) WritePacket(packet []byte) error {
	buf, err := a.session.AllocateSendPacket(len(packet))
	if err != nil {
		return err
	}
	copy(buf, packet)
	a.session.SendPacket(buf)
	return nil
}

func cidrPrefix(cidr string) (string, error) {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil || ip.To4() == nil {
		return "", fmt.Errorf("bad ipv4 cidr %q", cidr)
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return "", fmt.Errorf("bad ipv4 cidr %q", cidr)
	}
	return fmt.Sprint(ones), nil
}

func runNetsh(args ...string) error {
	cmd := exec.Command("netsh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isDuplicateRouteError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "object already exists") || strings.Contains(msg, "已存在")
}

func listRoutes() ([]vnet.Route, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command",
		"Get-NetRoute -AddressFamily IPv4 | Select-Object DestinationPrefix,InterfaceAlias | ConvertTo-Csv -NoTypeInformation")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	r := csv.NewReader(strings.NewReader(string(out)))
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse routes: %w", err)
	}
	routes := make([]vnet.Route, 0, len(rows))
	for i, row := range rows {
		if i == 0 || len(row) < 2 {
			continue
		}
		routes = append(routes, vnet.Route{CIDR: row[0], Interface: row[1]})
	}
	return routes, nil
}

func cleanup(cfg Config) error {
	cfg = normalizeConfig(cfg)
	if strings.TrimSpace(cfg.CIDR) != "" {
		if err := runNetsh("interface", "ipv4", "delete", "route", cfg.CIDR, cfg.Name); err != nil && !isMissingRouteError(err) {
			return err
		}
	}
	return nil
}

func isMissingRouteError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "element not found") || strings.Contains(msg, "找不到") || strings.Contains(msg, "不存在")
}
