package packet

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	TypeIPv4 byte = 1

	frameVersion byte = 1
	headerLen         = 4 + 1 + 1 + 1 + 1 + 8 + 8

	replayWindowSize uint64 = 64
)

var (
	magic = [4]byte{'U', 'D', 'P', 'L'}

	ErrBadKey = errors.New("bad packet session key")
	ErrFrame  = errors.New("bad packet frame")
	ErrReplay = errors.New("replayed packet")
)

type Direction int

const (
	DirectionAB Direction = iota
	DirectionBA
)

type SessionKeys struct {
	AB [32]byte
	BA [32]byte
}

type Codec struct {
	networkID uint64
	typ       byte
	aead      cipherAEAD
	nextSeq   uint64
	replay    replayWindow
}

type cipherAEAD interface {
	Seal(dst, nonce, plaintext, additionalData []byte) []byte
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
	Overhead() int
}

func DeriveSessionKeys(sharedSecret []byte, networkID uint64, sessionID, deviceA, deviceB string) (SessionKeys, error) {
	if len(sharedSecret) == 0 {
		return SessionKeys{}, ErrBadKey
	}
	devices := []string{deviceA, deviceB}
	sort.Strings(devices)
	salt := make([]byte, 0, 8+len(sessionID)+len(devices[0])+len(devices[1])+2)
	salt = binary.BigEndian.AppendUint64(salt, networkID)
	salt = append(salt, 0)
	salt = append(salt, sessionID...)
	salt = append(salt, 0)
	salt = append(salt, devices[0]...)
	salt = append(salt, 0)
	salt = append(salt, devices[1]...)

	ab, err := hkdfKey(sharedSecret, salt, []byte("udp-tunnel-lan/session/v1/a-to-b"))
	if err != nil {
		return SessionKeys{}, err
	}
	ba, err := hkdfKey(sharedSecret, salt, []byte("udp-tunnel-lan/session/v1/b-to-a"))
	if err != nil {
		return SessionKeys{}, err
	}
	return SessionKeys{AB: ab, BA: ba}, nil
}

func NewCodec(key [32]byte, networkID uint64, typ byte) (*Codec, error) {
	aead, err := chacha20poly1305.New(key[:])
	if err != nil {
		return nil, err
	}
	if typ == 0 {
		typ = TypeIPv4
	}
	return &Codec{networkID: networkID, typ: typ, aead: aead, nextSeq: 1}, nil
}

func (c *Codec) Seal(payload []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrBadKey
	}
	seq := c.nextSeq
	c.nextSeq++
	header := c.header(seq)
	nonce := nonceFrom(c.networkID, seq)
	out := make([]byte, 0, len(header)+len(payload)+c.aead.Overhead())
	out = append(out, header...)
	out = c.aead.Seal(out, nonce[:], payload, header)
	return out, nil
}

func (c *Codec) Open(frame []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrBadKey
	}
	if len(frame) < headerLen+c.aead.Overhead() || string(frame[:4]) != string(magic[:]) || frame[4] != frameVersion {
		return nil, ErrFrame
	}
	if frame[5] != c.typ {
		return nil, ErrFrame
	}
	networkID := binary.BigEndian.Uint64(frame[8:16])
	if networkID != c.networkID {
		return nil, ErrFrame
	}
	seq := binary.BigEndian.Uint64(frame[16:24])
	if !c.replay.Accept(seq) {
		return nil, ErrReplay
	}
	header := frame[:headerLen]
	nonce := nonceFrom(networkID, seq)
	plain, err := c.aead.Open(nil, nonce[:], frame[headerLen:], header)
	if err != nil {
		c.replay.Rollback(seq)
		return nil, fmt.Errorf("%w: %v", ErrFrame, err)
	}
	return plain, nil
}

func (c *Codec) header(seq uint64) []byte {
	header := make([]byte, headerLen)
	copy(header[:4], magic[:])
	header[4] = frameVersion
	header[5] = c.typ
	header[6] = 0
	header[7] = 0
	binary.BigEndian.PutUint64(header[8:16], c.networkID)
	binary.BigEndian.PutUint64(header[16:24], seq)
	return header
}

type replayWindow struct {
	max  uint64
	bits uint64
}

func (w *replayWindow) Accept(seq uint64) bool {
	if seq == 0 {
		return false
	}
	if w.max == 0 {
		w.max = seq
		w.bits = 1
		return true
	}
	if seq > w.max {
		shift := seq - w.max
		if shift >= replayWindowSize {
			w.bits = 1
		} else {
			w.bits = (w.bits << shift) | 1
		}
		w.max = seq
		return true
	}
	delta := w.max - seq
	if delta >= replayWindowSize || (w.bits&(uint64(1)<<delta)) != 0 {
		return false
	}
	w.bits |= uint64(1) << delta
	return true
}

func (w *replayWindow) Rollback(seq uint64) {
	if w.max == seq {
		w.bits &^= 1
		return
	}
	if w.max > seq {
		delta := w.max - seq
		if delta < replayWindowSize {
			w.bits &^= uint64(1) << delta
		}
	}
}

func nonceFrom(networkID, seq uint64) [12]byte {
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[:8], networkID)
	binary.BigEndian.PutUint32(nonce[8:], uint32(seq))
	return nonce
}

func hkdfKey(secret, salt, info []byte) ([32]byte, error) {
	var key [32]byte
	r := hkdf.New(sha256.New, secret, salt, info)
	if _, err := r.Read(key[:]); err != nil {
		return key, fmt.Errorf("derive packet key: %w", err)
	}
	return key, nil
}
