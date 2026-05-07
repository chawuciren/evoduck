package gateway

import (
	"net/http"
	"strings"

	"github.com/chawuciren/evoduck/pkg/logger"
)

func (g *Gateway) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.config.Gateway.Token == "" {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			auth = r.URL.Query().Get("token")
		}

		auth = strings.TrimPrefix(auth, "Bearer ")

		if auth != g.config.Gateway.Token {
			logger.Warn("Unauthorized request", logger.Fields{
				"method":      r.Method,
				"path":        r.URL.Path,
				"remote":      r.RemoteAddr,
				"status_code": http.StatusUnauthorized,
			})
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
