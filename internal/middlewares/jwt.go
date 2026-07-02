package middlewares

import (
	"crypto/rsa"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

func JWTAuth(publicKey *rsa.PublicKey) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Del("X-User-Id")
			r.Header.Del("X-User-Role")

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error": "Unauthorized: Missing header Authorization"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, `{"error": "Unauthorized: Format token invalid, must be Bearer <token>"}`, http.StatusUnauthorized)
				return
			}
			tokenString := parts[1]

			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf("invalid signing method: %v", token.Header["alg"])
				}
				return publicKey, nil
			}, jwt.WithExpirationRequired())

			if err != nil || !token.Valid {
				http.Error(w, `{"error": "Unauthorized: Token invalid or expired"}`, http.StatusUnauthorized)
				return
			}

			if claims, ok := token.Claims.(jwt.MapClaims); ok {
				if userID, exists := claims["user_id"]; exists {
					r.Header.Set("X-User-Id", fmt.Sprintf("%v", userID))
				}
				if role, exists := claims["role"]; exists {
					r.Header.Set("X-User-Role", fmt.Sprintf("%v", role))
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
