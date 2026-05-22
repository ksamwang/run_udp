package lan

import (
	"crypto/sha256"
	"encoding/base32"
	"os"
	"strings"
)

func StableDeviceID(seed string) string {
	seed = strings.ToLower(strings.TrimSpace(seed))
	if seed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(seed))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	return "DEV-" + encoded[:16]
}

func DeviceID() string {
	if id := StableDeviceID(machineUUID()); id != "" {
		return id
	}
	host, _ := os.Hostname()
	return StableDeviceID(host)
}
