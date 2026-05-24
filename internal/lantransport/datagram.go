package lantransport

import (
	"errors"
	"net"

	"udp_tunnel_demo/internal/packet"
)

var ErrUnavailable = errors.New("LAN datagram transport unavailable")

func IsFrame(data []byte) bool {
	return len(data) >= 4 && data[0] == 'U' && data[1] == 'D' && data[2] == 'P' && data[3] == 'L'
}

func Seal(codec *packet.Codec, payload []byte) ([]byte, error) {
	if codec == nil {
		return nil, ErrUnavailable
	}
	return codec.Seal(payload)
}

func Open(codec *packet.Codec, frame []byte) ([]byte, error) {
	if codec == nil {
		return nil, ErrUnavailable
	}
	return codec.Open(frame)
}

func Send(conn *net.UDPConn, dst *net.UDPAddr, codec *packet.Codec, payload []byte) error {
	if conn == nil || dst == nil || codec == nil {
		return ErrUnavailable
	}
	sealed, err := Seal(codec, payload)
	if err != nil {
		return err
	}
	_, err = conn.WriteToUDP(sealed, dst)
	return err
}
