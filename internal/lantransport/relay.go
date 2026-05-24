package lantransport

import (
	"encoding/binary"
	"errors"
)

var (
	ErrRelayFrame = errors.New("bad LAN relay frame")
	relayMagic    = [4]byte{'U', 'L', 'R', '1'}
)

type RelayFrame struct {
	SrcDevice string
	DstDevice string
	Payload   []byte
}

func IsRelayFrame(data []byte) bool {
	return len(data) >= 4 && data[0] == relayMagic[0] && data[1] == relayMagic[1] && data[2] == relayMagic[2] && data[3] == relayMagic[3]
}

func PackRelayFrame(frame RelayFrame) ([]byte, error) {
	if frame.SrcDevice == "" || frame.DstDevice == "" || len(frame.Payload) == 0 || len(frame.SrcDevice) > 255 || len(frame.DstDevice) > 255 {
		return nil, ErrRelayFrame
	}
	out := make([]byte, 8+len(frame.SrcDevice)+len(frame.DstDevice)+len(frame.Payload))
	copy(out[:4], relayMagic[:])
	binary.BigEndian.PutUint16(out[4:6], uint16(len(frame.SrcDevice)))
	binary.BigEndian.PutUint16(out[6:8], uint16(len(frame.DstDevice)))
	off := 8
	copy(out[off:], frame.SrcDevice)
	off += len(frame.SrcDevice)
	copy(out[off:], frame.DstDevice)
	off += len(frame.DstDevice)
	copy(out[off:], frame.Payload)
	return out, nil
}

func UnpackRelayFrame(data []byte) (RelayFrame, error) {
	if !IsRelayFrame(data) || len(data) < 8 {
		return RelayFrame{}, ErrRelayFrame
	}
	srcLen := int(binary.BigEndian.Uint16(data[4:6]))
	dstLen := int(binary.BigEndian.Uint16(data[6:8]))
	off := 8
	if srcLen <= 0 || dstLen <= 0 || len(data) <= off+srcLen+dstLen {
		return RelayFrame{}, ErrRelayFrame
	}
	src := string(data[off : off+srcLen])
	off += srcLen
	dst := string(data[off : off+dstLen])
	off += dstLen
	payload := append([]byte(nil), data[off:]...)
	if len(payload) == 0 {
		return RelayFrame{}, ErrRelayFrame
	}
	return RelayFrame{SrcDevice: src, DstDevice: dst, Payload: payload}, nil
}
