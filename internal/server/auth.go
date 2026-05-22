package server

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"udp_tunnel_demo/internal/store"
)

const defaultAdminUsername = "admin"
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

func (a *App) ensureAdminUser() error {
	ctx := rctx()
	if _, err := a.db.GetAdminUserByUsername(ctx, defaultAdminUsername); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if legacy, _ := a.db.GetMeta(ctx, "admin_password_hash"); legacy != "" {
		if err := a.db.UpsertAdminUser(ctx, store.AdminUser{
			ID:                  defaultAdminUsername,
			Username:            defaultAdminUsername,
			Name:                "Administrator",
			Role:                "admin",
			ForcePasswordChange: true,
			PasswordHash:        legacy,
		}); err != nil {
			return err
		}
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return a.db.UpsertAdminUser(ctx, store.AdminUser{
		ID:                  defaultAdminUsername,
		Username:            defaultAdminUsername,
		Name:                "Administrator",
		Role:                "admin",
		ForcePasswordChange: true,
		PasswordHash:        string(hash),
	})
}
