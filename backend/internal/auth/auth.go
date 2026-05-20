package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"mini-paperclip/backend/internal/httpx"
)

func Bearer(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.TrimSpace(token) == "" {
				httpx.Error(w, http.StatusInternalServerError, "auth_not_configured", "MP_API_TOKEN is required")
				return
			}
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				httpx.Error(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
