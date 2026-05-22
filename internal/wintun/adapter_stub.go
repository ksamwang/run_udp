//go:build !windows

package wintun

func openOrCreate(cfg Config) (*Adapter, error) {
	return nil, ErrUnsupported
}
