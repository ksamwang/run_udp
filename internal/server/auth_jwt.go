package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"udp_tunnel_demo/internal/store"
)

type tokenResponse struct {
	AccessToken         string         `json:"access_token"`
	AccessExpiresAt     string         `json:"access_expires_at"`
	RefreshToken        string         `json:"refresh_token,omitempty"`
	RefreshExpiresAt    string         `json:"refresh_expires_at,omitempty"`
	ForcePasswordChange bool           `json:"force_password_change,omitempty"`
	PasswordVersion     int64          `json:"password_version,omitempty"`
	User                map[string]any `json:"user"`
}

func (a *App) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = defaultAdminUsername
	}
	user, err := a.db.GetAdminUserByUsername(r.Context(), username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	resp, err := a.issueAdminTokenPair(r.Context(), r, user)
	writeJSONOrError(w, resp, err)
}

func (a *App) handleAdminRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.RefreshToken) == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	hash := hashRefreshToken(req.RefreshToken)
	stored, err := a.db.GetAdminRefreshToken(r.Context(), hash)
	if err != nil || stored.RevokedAt != "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	exp, err := time.Parse(time.RFC3339, stored.ExpiresAt)
	if err != nil || time.Now().After(exp) {
		_ = a.db.RevokeAdminRefreshToken(r.Context(), hash)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_ = a.db.TouchAdminRefreshToken(r.Context(), stored.ID)
	_ = a.db.RevokeAdminRefreshToken(r.Context(), hash)
	user, err := a.db.GetAdminUserByID(r.Context(), stored.UserID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	resp, err := a.issueAdminTokenPair(r.Context(), r, user)
	writeJSONOrError(w, resp, err)
}

func (a *App) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if strings.TrimSpace(req.RefreshToken) != "" {
		_ = a.db.RevokeAdminRefreshToken(r.Context(), hashRefreshToken(req.RefreshToken))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		claims, err := a.verifyAccessToken(token)
		if err != nil || claims.Subject == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user, err := a.db.GetAdminUserByID(r.Context(), claims.Subject)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if user.PasswordVersion != claims.PasswordVersion {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), adminClaimsKey{}, claims)))
	}
}

func (a *App) issueAdminTokenPair(ctx context.Context, r *http.Request, user store.AdminUser) (tokenResponse, error) {
	accessTTL := a.cfg.AdminAccessTokenTTL
	if accessTTL <= 0 {
		accessTTL = time.Hour
	}
	refreshTTL := a.cfg.AdminRefreshTokenTTL
	if refreshTTL <= 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
	accessExp := time.Now().Add(accessTTL)
	refreshExp := time.Now().Add(refreshTTL)
	access, err := a.signAccessToken(adminClaims{
		Subject:         user.ID,
		Role:            user.Role,
		PasswordVersion: user.PasswordVersion,
		Issued:          time.Now().Unix(),
		Expires:         accessExp.Unix(),
	})
	if err != nil {
		return tokenResponse{}, err
	}
	refresh, err := randomToken(32)
	if err != nil {
		return tokenResponse{}, err
	}
	if err := a.db.CreateAdminRefreshToken(ctx, user.ID, hashRefreshToken(refresh), refreshExp, r.UserAgent(), requestIP(r)); err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{
		AccessToken:         access,
		AccessExpiresAt:     accessExp.Format(time.RFC3339),
		RefreshToken:        refresh,
		RefreshExpiresAt:    refreshExp.Format(time.RFC3339),
		ForcePasswordChange: user.ForcePasswordChange,
		PasswordVersion:     user.PasswordVersion,
		User:                adminUser(user),
	}, nil
}

type adminClaims struct {
	Subject         string `json:"sub"`
	Role            string `json:"role"`
	PasswordVersion int64  `json:"pv"`
	Issued          int64  `json:"iat"`
	Expires         int64  `json:"exp"`
}

type adminClaimsKey struct{}

func (a *App) signAccessToken(claims adminClaims) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	h, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	p, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := jwtPart(h) + "." + jwtPart(p)
	sig := hmacSHA256([]byte(unsigned), a.adminJWTSecret())
	return unsigned + "." + jwtPart(sig), nil
}

func (a *App) verifyAccessToken(token string) (adminClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return adminClaims{}, errors.New("bad token")
	}
	unsigned := parts[0] + "." + parts[1]
	want := jwtPart(hmacSHA256([]byte(unsigned), a.adminJWTSecret()))
	if subtle.ConstantTimeCompare([]byte(parts[2]), []byte(want)) != 1 {
		return adminClaims{}, errors.New("bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return adminClaims{}, err
	}
	var claims adminClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return adminClaims{}, err
	}
	if claims.Expires <= time.Now().Unix() {
		return adminClaims{}, errors.New("expired")
	}
	return claims, nil
}

func (a *App) adminJWTSecret() []byte {
	secret := strings.TrimSpace(a.cfg.AdminJWTSecret)
	if secret != "" {
		return []byte(secret)
	}
	if a.cfg.PSK != "" {
		return hmacSHA256([]byte("admin-jwt"), []byte(a.cfg.PSK))
	}
	return []byte("udp-tunnel-dev-admin-jwt-secret")
}

func bearerToken(r *http.Request) string {
	v := strings.TrimSpace(r.Header.Get("Authorization"))
	if v == "" {
		return ""
	}
	prefix := "Bearer "
	if len(v) < len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(v[len(prefix):])
}

func jwtPart(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func hmacSHA256(data, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func adminUser(user store.AdminUser) map[string]any {
	name := user.Name
	if name == "" {
		name = user.Username
	}
	return map[string]any{
		"id":                    user.ID,
		"username":              user.Username,
		"name":                  name,
		"role":                  user.Role,
		"force_password_change": user.ForcePasswordChange,
	}
}

func requestIP(r *http.Request) string {
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xf != "" {
		if ip, _, ok := strings.Cut(xf, ","); ok {
			return strings.TrimSpace(ip)
		}
		return xf
	}
	host, _, ok := strings.Cut(r.RemoteAddr, ":")
	if !ok {
		return r.RemoteAddr
	}
	return host
}

func (a *App) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	claims, _ := r.Context().Value(adminClaimsKey{}).(adminClaims)
	user, err := a.db.GetAdminUserByID(r.Context(), claims.Subject)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if user.PasswordVersion != claims.PasswordVersion {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": adminUser(user)})
}
