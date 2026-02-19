package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func Middleware(internalToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Public routes
		if strings.HasPrefix(path, "/webhook/") || strings.HasPrefix(path, "/auth/") || path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		// Protected routes require token
		if strings.HasPrefix(path, "/api/") {
			token := r.Header.Get("X-Relay-Token")
			if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(internalToken)) != 1 {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
