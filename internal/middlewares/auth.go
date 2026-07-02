package middlewares

import (
	"crypto/subtle"
	"net/http"
)

func APIKeyAuth(validKey string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-API-Key")

			if key == "" || subtle.ConstantTimeCompare([]byte(key), []byte(validKey)) != 1 {
				http.Error(w, `{"error": "Unauthorized: Invalid or missing API Key"}`, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
