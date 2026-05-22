//go:build !windows

package wintun

import "udp_tunnel_demo/internal/vnet"

func openOrCreate(cfg Config) (*Adapter, error) {
	return nil, ErrUnsupported
}

func listRoutes() ([]vnet.Route, error) {
	return nil, ErrUnsupported
}

func cleanup(cfg Config) error {
	return ErrUnsupported
}
