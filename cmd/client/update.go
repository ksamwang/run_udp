package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"udp_tunnel_demo/internal/config"
)

func checkForUpdates(cfg config.Client, configPath string, requireUpgrade bool) (*releaseInfo, error) {
	appRuntime.SetUpdateStatus("checking", time.Now().Format(time.RFC3339), "")
	rel, err := fetchReleaseInfo(cfg)
	if err != nil {
		appRuntime.SetUpdateStatus("error", time.Now().Format(time.RFC3339), err.Error())
		return nil, err
	}
	if rel.Version == "" || rel.URL == "" {
		if requireUpgrade {
			return nil, errors.New("release info incomplete")
		}
		appRuntime.SetUpdateStatus("idle", time.Now().Format(time.RFC3339), "")
		return nil, nil
	}
	if compareVersionStrings(rel.Version, Version) <= 0 {
		appRuntime.SetUpdateStatus("up-to-date", time.Now().Format(time.RFC3339), "")
		return &rel, nil
	}
	pkgPath, err := downloadReleaseInstaller(rel)
	if err != nil {
		appRuntime.SetUpdateStatus("error", time.Now().Format(time.RFC3339), err.Error())
		return nil, err
	}
	if err := launchUpdater(pkgPath); err != nil {
		appRuntime.SetUpdateStatus("error", time.Now().Format(time.RFC3339), err.Error())
		return nil, err
	}
	appRuntime.SetUpdateStatus("installing", time.Now().Format(time.RFC3339), "")
	return &rel, nil
}

func fetchReleaseInfo(cfg config.Client) (releaseInfo, error) {
	u := strings.TrimRight(cfg.ServerHTTP, "/") + "/api/client/release"
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("X-UDP-Tunnel-PSK", cfg.PSK)
	res, err := doAgentHTTPRequest(req)
	if err != nil {
		return releaseInfo{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return releaseInfo{}, fmt.Errorf("http %d", res.StatusCode)
	}
	var rel releaseInfo
	return rel, json.NewDecoder(res.Body).Decode(&rel)
}

func downloadReleaseInstaller(rel releaseInfo) (string, error) {
	u, err := url.Parse(rel.URL)
	if err != nil {
		return "", err
	}
	res, err := http.Get(u.String())
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("download http %d", res.StatusCode)
	}
	name := filepath.Base(u.Path)
	if name == "" || name == "." || name == "/" {
		name = "udp-tunnel-client-setup.exe"
	}
	dst := filepath.Join(os.TempDir(), name)
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), res.Body); err != nil {
		return "", err
	}
	if rel.SHA256 != "" {
		sum := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(sum, rel.SHA256) {
			return "", fmt.Errorf("sha256 mismatch: got %s want %s", sum, rel.SHA256)
		}
	}
	return dst, nil
}

func compareVersionStrings(a, b string) int {
	parse := func(s string) []int {
		s = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(s)), "v")
		parts := strings.Split(s, ".")
		out := make([]int, 0, len(parts))
		for _, p := range parts {
			n, _ := strconv.Atoi(strings.TrimSpace(p))
			out = append(out, n)
		}
		return out
	}
	aa, bb := parse(a), parse(b)
	max := len(aa)
	if len(bb) > max {
		max = len(bb)
	}
	for i := 0; i < max; i++ {
		var av, bv int
		if i < len(aa) {
			av = aa[i]
		}
		if i < len(bb) {
			bv = bb[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}
