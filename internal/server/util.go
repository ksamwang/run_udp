package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"udp_tunnel_demo/internal/store"
)

type apiError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"error"`
}

func (e *apiError) Error() string { return e.Message }

func badRequest(code, message string) error {
	return &apiError{Status: http.StatusBadRequest, Code: code, Message: message}
}

func writeJSONOrError(w http.ResponseWriter, v any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			writeJSON(w, apiErr.Status, apiErr)
			return
		}
		if errors.Is(err, sqlErrNoRows()) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"code": "internal_error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func requestAddr(r *http.Request) string {
	host, port, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return net.JoinHostPort(host, port)
}

func externalUDPAddr(r *http.Request, udpListen string) string {
	host, _, _ := net.SplitHostPort(r.Host)
	if host == "" {
		host = r.Host
	}
	udpHost, udpPort, err := net.SplitHostPort(udpListen)
	if err != nil {
		return net.JoinHostPort(host, "7000")
	}
	switch udpHost {
	case "", "0.0.0.0", "::":
		udpHost = host
	}
	return net.JoinHostPort(udpHost, udpPort)
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); xf != "" {
		scheme = xf
	}
	return scheme + "://" + r.Host
}

func portFromAddr(addr string, fallback int) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return fallback
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return fallback
	}
	return n
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func pairKey(a, b, profile string) string {
	if a < b {
		return a + "\x00" + b + "\x00" + store.NormalizeProfile(profile)
	}
	return b + "\x00" + a + "\x00" + store.NormalizeProfile(profile)
}

func cloneUDP(a *net.UDPAddr) *net.UDPAddr {
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

func rctx() context.Context {
	return context.Background()
}

func sqlErrNoRows() error {
	return sql.ErrNoRows
}
