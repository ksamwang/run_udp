package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *App) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w.Header())
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) ginCORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		setCORSHeaders(c.Writer.Header())
		if c.Request.Method == http.MethodOptions {
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}
		c.Next()
	}
}

func setCORSHeaders(h http.Header) {
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-UDP-Tunnel-PSK")
}
