package lan

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func SignRegister(id Identity, from, peer, profile string, ts int64) (string, error) {
	return SignRegisterPayload(id, from, peer, profile, ts, "")
}

func SignRegisterPayload(id Identity, from, peer, profile string, ts int64, payload string) (string, error) {
	privRaw, err := base64.StdEncoding.DecodeString(id.PrivateKey)
	if err != nil || len(privRaw) != ed25519.PrivateKeySize {
		return "", errors.New("invalid lan private key")
	}
	sig := ed25519.Sign(ed25519.PrivateKey(privRaw), RegisterTranscriptPayload(from, peer, profile, ts, payload))
	return base64.StdEncoding.EncodeToString(sig), nil
}

func VerifyRegister(publicKey, from, peer, profile string, ts int64, signature string) error {
	return VerifyRegisterPayload(publicKey, from, peer, profile, ts, "", signature)
}

func VerifyRegisterPayload(publicKey, from, peer, profile string, ts int64, payload string, signature string) error {
	pubRaw, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(pubRaw) != ed25519.PublicKeySize {
		return errors.New("invalid lan public key")
	}
	sigRaw, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sigRaw) != ed25519.SignatureSize {
		return errors.New("invalid lan signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pubRaw), RegisterTranscriptPayload(from, peer, profile, ts, payload), sigRaw) {
		return errors.New("bad lan signature")
	}
	return nil
}

func RegisterTranscript(from, peer, profile string, ts int64) []byte {
	return RegisterTranscriptPayload(from, peer, profile, ts, "")
}

func RegisterTranscriptPayload(from, peer, profile string, ts int64, payload string) []byte {
	parts := []string{
		"udp-tunnel-lan/register/v1",
		strings.TrimSpace(from),
		strings.TrimSpace(peer),
		strings.TrimSpace(profile),
		strconv.FormatInt(ts, 10),
		strings.TrimSpace(payload),
	}
	return []byte(strings.Join(parts, "\n"))
}

func MustSignRegister(id Identity, from, peer, profile string, ts int64) string {
	sig, err := SignRegister(id, from, peer, profile, ts)
	if err != nil {
		panic(fmt.Sprintf("sign lan register: %v", err))
	}
	return sig
}
