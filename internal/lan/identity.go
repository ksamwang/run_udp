package lan

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	IdentityKeyAlgorithm = "ed25519"
	identityFileName     = "lan-identity.json"
)

type Identity struct {
	Algorithm  string `json:"algorithm"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

func LoadOrCreateIdentity(configPath string) (Identity, error) {
	path := identityPath(configPath)
	id, err := LoadIdentity(path)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Identity{}, err
	}
	id, err = GenerateIdentity()
	if err != nil {
		return Identity{}, err
	}
	return id, SaveIdentity(path, id)
}

func GenerateIdentity() (Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, fmt.Errorf("generate lan identity: %w", err)
	}
	return Identity{
		Algorithm:  IdentityKeyAlgorithm,
		PrivateKey: base64.StdEncoding.EncodeToString(priv),
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
	}, nil
}

func LoadIdentity(path string) (Identity, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Identity{}, err
	}
	var id Identity
	if err := json.Unmarshal(b, &id); err != nil {
		return Identity{}, fmt.Errorf("decode lan identity %s: %w", path, err)
	}
	if err := ValidateIdentity(id); err != nil {
		return Identity{}, fmt.Errorf("decode lan identity %s: %w", path, err)
	}
	return id, nil
}

func SaveIdentity(path string, id Identity) error {
	if err := ValidateIdentity(id); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create lan identity dir: %w", err)
	}
	b, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return fmt.Errorf("encode lan identity: %w", err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0600); err != nil {
		return fmt.Errorf("write lan identity %s: %w", path, err)
	}
	return nil
}

func ValidateIdentity(id Identity) error {
	if id.Algorithm != IdentityKeyAlgorithm {
		return fmt.Errorf("unsupported lan identity algorithm %q", id.Algorithm)
	}
	priv, err := base64.StdEncoding.DecodeString(id.PrivateKey)
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		return errors.New("invalid lan private key")
	}
	pub, err := base64.StdEncoding.DecodeString(id.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid lan public key")
	}
	derived := ed25519.PrivateKey(priv).Public().(ed25519.PublicKey)
	if string(derived) != string(pub) {
		return errors.New("lan public key does not match private key")
	}
	return nil
}

func identityPath(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return identityFileName
	}
	dir := filepath.Dir(configPath)
	if dir == "." || dir == "" {
		return identityFileName
	}
	return filepath.Join(dir, identityFileName)
}
