package wintun

import (
	"errors"
	"net"

	"udp_tunnel_demo/internal/vnet"
)

const (
	DefaultAdapterName = "UDP Tunnel LAN"
	DefaultMTU         = 1280
	DefaultRingSize    = 16 * 1024 * 1024
)

var ErrUnsupported = errors.New("wintun is only supported on windows")

type Config struct {
	Name string
	IP   net.IP
	CIDR string
	MTU  int
}

type Adapter struct {
	impl adapterImpl
}

type SystemState struct {
	Routes       []vnet.Route  `json:"routes"`
	Conflict     vnet.Conflict `json:"conflict"`
	SelectedCIDR string        `json:"selected_cidr"`
	MSS          int           `json:"mss"`
}

func OpenOrCreate(cfg Config) (*Adapter, error) {
	return openOrCreate(cfg)
}

func InspectSystem(preferredCIDR string, mtu int) (SystemState, error) {
	routes, err := listRoutes()
	if err != nil {
		return SystemState{}, err
	}
	conflict, err := vnet.DetectConflict(preferredCIDR, routes)
	if err != nil {
		return SystemState{}, err
	}
	selected, err := vnet.NextAvailableCIDR(preferredCIDR, routes)
	if err != nil {
		return SystemState{}, err
	}
	return SystemState{Routes: routes, Conflict: conflict, SelectedCIDR: selected, MSS: vnet.MSSForMTU(mtu)}, nil
}

func Cleanup(cfg Config) error {
	return cleanup(cfg)
}

func (a *Adapter) Close() error {
	if a == nil || a.impl == nil {
		return nil
	}
	return a.impl.Close()
}

func (a *Adapter) Configure(cfg Config) error {
	if a == nil || a.impl == nil {
		return ErrUnsupported
	}
	return a.impl.Configure(cfg)
}

func (a *Adapter) ReadPacket() ([]byte, error) {
	if a == nil || a.impl == nil {
		return nil, ErrUnsupported
	}
	return a.impl.ReadPacket()
}

func (a *Adapter) WritePacket(packet []byte) error {
	if a == nil || a.impl == nil {
		return ErrUnsupported
	}
	return a.impl.WritePacket(packet)
}

type adapterImpl interface {
	Close() error
	Configure(Config) error
	ReadPacket() ([]byte, error)
	WritePacket([]byte) error
}

func normalizeConfig(cfg Config) Config {
	if cfg.Name == "" {
		cfg.Name = DefaultAdapterName
	}
	if cfg.MTU <= 0 {
		cfg.MTU = DefaultMTU
	}
	return cfg
}
