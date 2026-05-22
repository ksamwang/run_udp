package packet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"udp_tunnel_demo/internal/store"
	"udp_tunnel_demo/internal/vnet"
)

const (
	IPv4ProtocolICMP byte = 1
	IPv4ProtocolTCP  byte = 6
	IPv4ProtocolUDP  byte = 17

	DropACLDeny             = "acl_deny"
	DropRouteMiss           = "route_miss"
	DropMTU                 = "mtu_drop"
	DropPeerUnavailable     = "peer_unavailable"
	DropInvalidPacket       = "invalid_packet"
	DropUnsupportedProtocol = "unsupported_protocol"
)

var (
	ErrInvalidIPv4         = errors.New("invalid ipv4 packet")
	ErrRouteMiss           = errors.New("route miss")
	ErrACLDeny             = errors.New("acl deny")
	ErrMTUDrop             = errors.New("mtu drop")
	ErrPeerUnavailable     = errors.New("peer unavailable")
	ErrUnsupportedProtocol = errors.New("unsupported protocol")
)

type IPv4Header struct {
	HeaderLen   int
	TotalLen    int
	Protocol    byte
	SourceIP    net.IP
	DestIP      net.IP
	SourcePort  int
	DestPort    int
	TCPSYN      bool
	PayloadSize int
}

type RoutedFrame struct {
	NetworkID  int64      `json:"network_id"`
	SrcDevice  string     `json:"src_device"`
	DstDevice  string     `json:"dst_device"`
	PacketType byte       `json:"packet_type"`
	Payload    []byte     `json:"payload"`
	Header     IPv4Header `json:"-"`
}

type DropStats struct {
	ACLDeny             uint64 `json:"acl_deny"`
	RouteMiss           uint64 `json:"route_miss"`
	MTU                 uint64 `json:"mtu_drop"`
	PeerUnavailable     uint64 `json:"peer_unavailable"`
	InvalidPacket       uint64 `json:"invalid_packet"`
	UnsupportedProtocol uint64 `json:"unsupported_protocol"`
}

type RouterStats struct {
	TxBytes   uint64    `json:"tx_bytes"`
	RxBytes   uint64    `json:"rx_bytes"`
	TxPackets uint64    `json:"tx_packets"`
	RxPackets uint64    `json:"rx_packets"`
	Drops     DropStats `json:"drops"`
}

type RouterConfig struct {
	NetworkID      int64
	SourceDeviceID string
	MTU            int
	Addresses      []store.VirtualAddress
	ACLRules       []store.VirtualACLRule
	PeerAvailable  map[string]bool
}

type Router struct {
	networkID      int64
	sourceDeviceID string
	mtu            int
	routes         map[string]string
	acl            []store.VirtualACLRule
	peerAvailable  map[string]bool

	mu    sync.Mutex
	stats RouterStats
}

func NewRouter(cfg RouterConfig) (*Router, error) {
	if cfg.NetworkID <= 0 {
		return nil, errors.New("network_id is required")
	}
	if strings.TrimSpace(cfg.SourceDeviceID) == "" {
		return nil, errors.New("source device id is required")
	}
	mtu := cfg.MTU
	if mtu <= 0 {
		mtu = vnet.DefaultMTU
	}
	routes := make(map[string]string, len(cfg.Addresses))
	for _, address := range cfg.Addresses {
		if address.NetworkID != cfg.NetworkID || strings.TrimSpace(address.DeviceID) == "" {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(address.VirtualIP)).To4()
		if ip == nil {
			continue
		}
		routes[ip.String()] = address.DeviceID
	}
	peers := make(map[string]bool, len(cfg.PeerAvailable))
	for id, ok := range cfg.PeerAvailable {
		peers[id] = ok
	}
	return &Router{
		networkID: cfg.NetworkID, sourceDeviceID: cfg.SourceDeviceID, mtu: mtu,
		routes: routes, acl: append([]store.VirtualACLRule(nil), cfg.ACLRules...), peerAvailable: peers,
	}, nil
}

func (r *Router) RouteOutbound(packet []byte) (RoutedFrame, error) {
	if r == nil {
		return RoutedFrame{}, errors.New("router is nil")
	}
	if len(packet) > r.mtu {
		r.recordDrop(DropMTU)
		return RoutedFrame{}, ErrMTUDrop
	}
	header, err := ParseIPv4(packet)
	if err != nil {
		r.recordDrop(DropInvalidPacket)
		return RoutedFrame{}, err
	}
	switch header.Protocol {
	case IPv4ProtocolTCP:
	case IPv4ProtocolICMP:
		// ICMP is useful for diagnostics, but TCP remains the first product path.
	default:
		r.recordDrop(DropUnsupportedProtocol)
		return RoutedFrame{}, ErrUnsupportedProtocol
	}
	dstDevice := r.routes[header.DestIP.String()]
	if dstDevice == "" {
		r.recordDrop(DropRouteMiss)
		return RoutedFrame{}, ErrRouteMiss
	}
	if ok, exists := r.peerAvailable[dstDevice]; exists && !ok {
		r.recordDrop(DropPeerUnavailable)
		return RoutedFrame{}, ErrPeerUnavailable
	}
	if !r.allow(header, dstDevice) {
		r.recordDrop(DropACLDeny)
		return RoutedFrame{}, ErrACLDeny
	}
	payload := append([]byte(nil), packet[:header.TotalLen]...)
	if header.Protocol == IPv4ProtocolTCP {
		_ = ClampTCPMSS(payload, vnet.MSSForMTU(r.mtu))
		header, _ = ParseIPv4(payload)
	}
	frame := RoutedFrame{
		NetworkID: r.networkID, SrcDevice: r.sourceDeviceID, DstDevice: dstDevice,
		PacketType: TypeIPv4, Payload: payload, Header: header,
	}
	r.mu.Lock()
	r.stats.TxPackets++
	r.stats.TxBytes += uint64(len(payload))
	r.mu.Unlock()
	return frame, nil
}

func (r *Router) RecordInbound(frame RoutedFrame) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.stats.RxPackets++
	r.stats.RxBytes += uint64(len(frame.Payload))
	r.mu.Unlock()
}

func (r *Router) Stats() RouterStats {
	if r == nil {
		return RouterStats{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

func (r *Router) recordDrop(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch reason {
	case DropACLDeny:
		r.stats.Drops.ACLDeny++
	case DropRouteMiss:
		r.stats.Drops.RouteMiss++
	case DropMTU:
		r.stats.Drops.MTU++
	case DropPeerUnavailable:
		r.stats.Drops.PeerUnavailable++
	case DropUnsupportedProtocol:
		r.stats.Drops.UnsupportedProtocol++
	default:
		r.stats.Drops.InvalidPacket++
	}
}

func (r *Router) allow(header IPv4Header, dstDevice string) bool {
	allowed := true
	for _, rule := range r.acl {
		if !rule.Enabled || rule.NetworkID != r.networkID || !matchACLRule(rule, r.networkID, r.sourceDeviceID, dstDevice, header) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(rule.Action), "deny") {
			return false
		}
		if strings.EqualFold(strings.TrimSpace(rule.Action), "allow") || strings.TrimSpace(rule.Action) == "" {
			allowed = true
		}
	}
	return allowed
}

func matchACLRule(rule store.VirtualACLRule, networkID int64, srcDevice, dstDevice string, header IPv4Header) bool {
	if rule.SourceDeviceID != "" && rule.SourceDeviceID != srcDevice {
		return false
	}
	if rule.TargetDeviceID != "" && rule.TargetDeviceID != dstDevice {
		return false
	}
	if rule.SourceGroupID != "" && rule.SourceGroupID != strconv.FormatInt(networkID, 10) {
		return false
	}
	if rule.TargetGroupID != "" && rule.TargetGroupID != strconv.FormatInt(networkID, 10) {
		return false
	}
	if !matchProtocol(rule.Protocol, header.Protocol) {
		return false
	}
	if rule.PortStart <= 0 && rule.PortEnd <= 0 {
		return true
	}
	if header.DestPort <= 0 {
		return false
	}
	start, end := rule.PortStart, rule.PortEnd
	if start <= 0 {
		start = end
	}
	if end <= 0 {
		end = start
	}
	return header.DestPort >= start && header.DestPort <= end
}

func matchProtocol(ruleProtocol string, packetProtocol byte) bool {
	switch strings.ToLower(strings.TrimSpace(ruleProtocol)) {
	case "", "any", "*":
		return true
	case "tcp":
		return packetProtocol == IPv4ProtocolTCP
	case "icmp":
		return packetProtocol == IPv4ProtocolICMP
	case "udp":
		return packetProtocol == IPv4ProtocolUDP
	default:
		n, err := strconv.Atoi(ruleProtocol)
		return err == nil && n >= 0 && n <= 255 && byte(n) == packetProtocol
	}
}

func ParseIPv4(packet []byte) (IPv4Header, error) {
	if len(packet) < 20 {
		return IPv4Header{}, fmt.Errorf("%w: too short", ErrInvalidIPv4)
	}
	if packet[0]>>4 != 4 {
		return IPv4Header{}, fmt.Errorf("%w: bad version", ErrInvalidIPv4)
	}
	ihl := int(packet[0]&0x0f) * 4
	if ihl < 20 || len(packet) < ihl {
		return IPv4Header{}, fmt.Errorf("%w: bad header length", ErrInvalidIPv4)
	}
	totalLen := int(binary.BigEndian.Uint16(packet[2:4]))
	if totalLen < ihl || totalLen > len(packet) {
		return IPv4Header{}, fmt.Errorf("%w: bad total length", ErrInvalidIPv4)
	}
	header := IPv4Header{
		HeaderLen: ihl, TotalLen: totalLen, Protocol: packet[9],
		SourceIP: append(net.IP(nil), packet[12:16]...), DestIP: append(net.IP(nil), packet[16:20]...),
		PayloadSize: totalLen - ihl,
	}
	if header.Protocol == IPv4ProtocolTCP && totalLen >= ihl+20 {
		tcp := packet[ihl:totalLen]
		dataOffset := int(tcp[12]>>4) * 4
		if dataOffset >= 20 && len(tcp) >= dataOffset {
			header.SourcePort = int(binary.BigEndian.Uint16(tcp[0:2]))
			header.DestPort = int(binary.BigEndian.Uint16(tcp[2:4]))
			header.TCPSYN = tcp[13]&0x02 != 0
		}
	}
	return header, nil
}

func ClampTCPMSS(packet []byte, maxMSS int) bool {
	header, err := ParseIPv4(packet)
	if err != nil || header.Protocol != IPv4ProtocolTCP || !header.TCPSYN || maxMSS <= 0 {
		return false
	}
	tcp := packet[header.HeaderLen:header.TotalLen]
	dataOffset := int(tcp[12]>>4) * 4
	if dataOffset < 20 || dataOffset > len(tcp) {
		return false
	}
	changed := false
	for i := 20; i < dataOffset; {
		kind := tcp[i]
		switch kind {
		case 0:
			i = dataOffset
		case 1:
			i++
		default:
			if i+1 >= dataOffset {
				i = dataOffset
				continue
			}
			l := int(tcp[i+1])
			if l < 2 || i+l > dataOffset {
				i = dataOffset
				continue
			}
			if kind == 2 && l == 4 {
				mss := int(binary.BigEndian.Uint16(tcp[i+2 : i+4]))
				if mss > maxMSS {
					binary.BigEndian.PutUint16(tcp[i+2:i+4], uint16(maxMSS))
					changed = true
				}
			}
			i += l
		}
	}
	if changed {
		recomputeIPv4HeaderChecksum(packet[:header.HeaderLen])
		recomputeTCPChecksum(packet[:header.TotalLen], header)
	}
	return changed
}

func recomputeIPv4HeaderChecksum(header []byte) {
	header[10], header[11] = 0, 0
	binary.BigEndian.PutUint16(header[10:12], checksum(header))
}

func recomputeTCPChecksum(packet []byte, header IPv4Header) {
	tcp := packet[header.HeaderLen:header.TotalLen]
	tcp[16], tcp[17] = 0, 0
	pseudo := make([]byte, 0, 12+len(tcp)+1)
	pseudo = append(pseudo, packet[12:16]...)
	pseudo = append(pseudo, packet[16:20]...)
	pseudo = append(pseudo, 0, IPv4ProtocolTCP)
	pseudo = binary.BigEndian.AppendUint16(pseudo, uint16(len(tcp)))
	pseudo = append(pseudo, tcp...)
	if len(pseudo)%2 == 1 {
		pseudo = append(pseudo, 0)
	}
	binary.BigEndian.PutUint16(tcp[16:18], checksum(pseudo))
}

func checksum(data []byte) uint16 {
	var sum uint32
	for len(data) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
	}
	if len(data) > 0 {
		sum += uint32(data[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
