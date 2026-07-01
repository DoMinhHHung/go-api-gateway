package middlewares

import (
	"net/http"
)

func APIKeyAuth(validKey string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			key := r.Header.Get("X-API-Key")

			if key != validKey {
				http.Error(w, `{"error": "Unauthorized: Invalid or missing API Key"}`, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
