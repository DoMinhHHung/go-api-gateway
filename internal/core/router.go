package core

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/DoMinhHHung/go-api-gateway/internal/middlewares"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker"
)

func SetupRoutes(cfg *config.AppConfig, rdb *redis.Client) *http.ServeMux {
	mux := http.NewServeMux()
	registry := middlewares.InitRegistry(cfg)

	for _, route := range cfg.Routes {
		if len(route.Backends) == 0 {
			continue
		}

		targetURL, _ := url.Parse(route.Backends[0].URL)
		proxy := httputil.NewSingleHostReverseProxy(targetURL)

		proxy.Transport = &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			MaxConnsPerHost:       200,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
		}

		baseTransport := &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			MaxConnsPerHost:       200,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
		}

		proxy.Transport = NewBreakerTransport(baseTransport, route.ID, route.CircuitBreaker)

		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Content-Type", "application/json")
			if err == gobreaker.ErrOpenState {
				w.WriteHeader(http.StatusServiceUnavailable) // 503
				w.Write([]byte(`{"error": "Service Unavailable", "message": "Service is currently unavailable, please try again later!"}`))
				return
			}
			w.WriteHeader(http.StatusBadGateway) // Error 502
			w.Write([]byte(`{"error": "Bad Gateway", "message": "Can not connected to service!"}`))
		}

		for _, method := range route.Methods {
			pattern := method + " " + route.Path

			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxy.ServeHTTP(w, r)
			})

			if route.RateLimit.Enabled {
				handler = middlewares.RateLimit(rdb, route.ID, route.RateLimit)(handler)
			}

			if len(route.Middlewares) > 0 {
				handler = middlewares.ApplyChain(handler, route.Middlewares, registry)
			}

			mux.Handle(pattern, handler)
		}
	}

	return mux
}
