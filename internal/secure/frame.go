package secure

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	KindControl byte = 1
	KindKCP     byte = 2

	frameVersion byte = 1
	headerLen         = 4 + 1 + 1 + chacha20poly1305.NonceSizeX
)

var (
	magic         = [4]byte{'U', 'D', 'P', 'T'}
	ErrNotFrame   = errors.New("not secure frame")
	ErrBadFrame   = errors.New("bad secure frame")
	ErrBadPSK     = errors.New("missing psk")
	infoFrameKey  = []byte("udp-tunnel/frame-key/v1")
	infoConvIDKey = []byte("udp-tunnel/kcp-conv/v1")
)

type Codec struct {
	aead cipherAEAD
}

type cipherAEAD interface {
	Seal(dst, nonce, plaintext, additionalData []byte) []byte
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
}

func NewCodec(psk string) (*Codec, error) {
	if psk == "" {
		return nil, ErrBadPSK
	}
	key, err := derive(psk, infoFrameKey, chacha20poly1305.KeySize)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	return &Codec{aead: aead}, nil
}

func IsFrame(b []byte) bool {
	return len(b) >= 6 && b[0] == magic[0] && b[1] == magic[1] && b[2] == magic[2] && b[3] == magic[3]
}

func (c *Codec) Seal(kind byte, plaintext []byte) ([]byte, error) {
	if c == nil {
		return nil, ErrBadPSK
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	out := make([]byte, 0, headerLen+len(plaintext)+16)
	out = append(out, magic[:]...)
	out = append(out, frameVersion, kind)
	out = append(out, nonce...)
	ad := out[:headerLen]
	out = c.aead.Seal(out, nonce, plaintext, ad)
	return out, nil
}

func (c *Codec) Open(frame []byte) (byte, []byte, error) {
	if c == nil {
		return 0, nil, ErrBadPSK
	}
	if !IsFrame(frame) {
		return 0, nil, ErrNotFrame
	}
	if len(frame) < headerLen+16 || frame[4] != frameVersion {
		return 0, nil, ErrBadFrame
	}
	kind := frame[5]
	nonce := frame[6:headerLen]
	ad := frame[:headerLen]
	plain, err := c.aead.Open(nil, nonce, frame[headerLen:], ad)
	if err != nil {
		return 0, nil, err
	}
	return kind, plain, nil
}

func ConvID(psk, a, b, profile string) uint32 {
	ids := []string{a, b}
	sort.Strings(ids)
	mac := hmac.New(sha256.New, []byte(psk))
	mac.Write(infoConvIDKey)
	mac.Write([]byte{0})
	mac.Write([]byte(ids[0]))
	mac.Write([]byte{0})
	mac.Write([]byte(ids[1]))
	mac.Write([]byte{0})
	mac.Write([]byte(profile))
	sum := mac.Sum(nil)
	v := binary.BigEndian.Uint32(sum[:4])
	if v == 0 {
		return 1
	}
	return v
}

func derive(psk string, info []byte, size int) ([]byte, error) {
	r := hkdf.New(sha256.New, []byte(psk), nil, info)
	key := make([]byte, size)
	if _, err := r.Read(key); err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	return key, nil
}
