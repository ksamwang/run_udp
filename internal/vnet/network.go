package vnet

import (
	"encoding/binary"
	"fmt"
	"net"
)

const (
	DefaultCIDR = "172.16.10.0/24"
	DefaultMTU  = 1280
	DefaultMSS  = 1200
)

type Route struct {
	CIDR      string `json:"cidr"`
	Interface string `json:"interface,omitempty"`
}

type Conflict struct {
	CIDR      string `json:"cidr"`
	Existing  Route  `json:"existing"`
	Conflicts bool   `json:"conflicts"`
}

func DetectConflict(targetCIDR string, routes []Route) (Conflict, error) {
	_, target, err := net.ParseCIDR(targetCIDR)
	if err != nil {
		return Conflict{}, fmt.Errorf("bad cidr %q: %w", targetCIDR, err)
	}
	for _, route := range routes {
		if route.CIDR == "" {
			continue
		}
		_, existing, err := net.ParseCIDR(route.CIDR)
		if err != nil {
			continue
		}
		if isDefaultRoute(existing) {
			continue
		}
		if cidrOverlap(target, existing) {
			return Conflict{CIDR: targetCIDR, Existing: route, Conflicts: true}, nil
		}
	}
	return Conflict{CIDR: targetCIDR}, nil
}

func NextAvailableCIDR(preferred string, routes []Route) (string, error) {
	candidates := []string{preferred}
	for i := 10; i < 255; i++ {
		candidates = append(candidates, fmt.Sprintf("172.16.%d.0/24", i))
	}
	for _, candidate := range candidates {
		conflict, err := DetectConflict(candidate, routes)
		if err != nil {
			return "", err
		}
		if !conflict.Conflicts {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no available virtual cidr")
}

func MSSForMTU(mtu int) int {
	if mtu <= 0 {
		mtu = DefaultMTU
	}
	mss := mtu - 40
	if mss > DefaultMSS {
		return DefaultMSS
	}
	if mss < 536 {
		return 536
	}
	return mss
}

func cidrOverlap(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP) || ipNetLast(a).Equal(b.IP) || ipNetLast(b).Equal(a.IP)
}

func isDefaultRoute(n *net.IPNet) bool {
	ones, bits := n.Mask.Size()
	return bits == 32 && ones == 0
}

func ipNetLast(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	if ip == nil {
		return n.IP
	}
	mask := n.Mask
	out := make(net.IP, len(ip))
	v := binary.BigEndian.Uint32(ip)
	m := binary.BigEndian.Uint32(mask)
	binary.BigEndian.PutUint32(out, v|^m)
	return out
}
