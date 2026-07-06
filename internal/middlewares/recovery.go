package middlewares

import (
	"log/slog"
	"net/http"
)

func Recovery() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					slog.Error("panic recovered", "panic", recovered)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"Internal Server Error","message":"The gateway recovered from a panic."}`))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
