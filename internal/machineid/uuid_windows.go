//go:build windows

package machineid

import (
	"strings"

	"github.com/StackExchange/wmi"
)

type computerSystemProduct struct {
	UUID string
}

// UUID reads the machine UUID through WMI COM without spawning PowerShell.
func UUID() string {
	var rows []computerSystemProduct
	if err := wmi.Query("SELECT UUID FROM Win32_ComputerSystemProduct", &rows); err != nil {
		return ""
	}
	if len(rows) == 0 {
		return ""
	}
	uuid := strings.TrimSpace(rows[0].UUID)
	if !validUUID(uuid) {
		return ""
	}
	return uuid
}

func validUUID(uuid string) bool {
	if uuid == "" {
		return false
	}
	invalid := []string{
		"FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF",
		"00000000-0000-0000-0000-000000000000",
		"03000200-0400-0500-0006-000700080009",
	}
	for _, value := range invalid {
		if strings.EqualFold(uuid, value) {
			return false
		}
	}
	return true
}
