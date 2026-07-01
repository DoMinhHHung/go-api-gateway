package middlewares

import (
	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"log"
	"net/http"
)

type Middleware func(http.Handler) http.Handler

func InitRegistry(cfg *config.AppConfig) map[string]Middleware {
	return map[string]Middleware{
		"api_key": APIKeyAuth(cfg.Security.APIKey),
		"jwt":     JWTAuth(cfg.Security.JWTSecret),
	}
}

func ApplyChain(h http.Handler, middlewareNames []string, registry map[string]Middleware) http.Handler {
	for i := len(middlewareNames) - 1; i >= 0; i-- {
		name := middlewareNames[i]
		if mw, exists := registry[name]; exists {
			h = mw(h)
		} else {
			log.Printf("[Cảnh báo] Middleware '%s' không tồn tại, bỏ qua.", name)
		}
	}
	return h
}
