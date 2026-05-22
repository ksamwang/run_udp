package server

import (
	"crypto/subtle"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

const defaultAdminPassword = "admin"

func (a *App) requireAgent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.PSK != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-UDP-Tunnel-PSK")), []byte(a.cfg.PSK)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *App) ensureAdminPassword() error {
	if existing, _ := a.db.GetMeta(rctx(), "admin_password_hash"); existing != "" {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return a.db.PutMeta(rctx(), "admin_password_hash", string(hash))
}
