package core

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/DoMinhHHung/go-api-gateway/internal/middlewares"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker"
)

func newReverseProxy(target *url.URL, routeID string, breakerCfg config.CircuitBreakerConfig, responseHeaderTimeout time.Duration) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
	}

	baseTransport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		MaxConnsPerHost:       200,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
	}
	proxy.Transport = NewBreakerTransport(baseTransport, routeID, breakerCfg)

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		if err == gobreaker.ErrOpenState {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
			w.Write([]byte(`{"error": "Service Unavailable", "message": "Service is currently unavailable, please try again later!"}`))
			return
		}
		w.WriteHeader(http.StatusBadGateway) // 502
		w.Write([]byte(`{"error": "Bad Gateway", "message": "Can not connected to service!"}`))
	}

	return proxy
}

func SetupRoutes(cfg *config.AppConfig, rdb *redis.Client) *http.ServeMux {
	mux := http.NewServeMux()
	registry := middlewares.InitRegistry(cfg)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := rdb.Ping(ctx).Err(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"redis unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	for _, route := range cfg.Routes {
		if len(route.Backends) == 0 {
			continue
		}

		targetURL, _ := url.Parse(route.Backends[0].URL)
		proxy := newReverseProxy(targetURL, route.ID, route.CircuitBreaker, route.EffectiveResponseHeaderTimeout())

		var handler http.Handler = proxy

		if route.RateLimit.Enabled {
			handler = middlewares.RateLimit(rdb, route.ID, route.RateLimit, cfg.Server.TrustProxyHeaders)(handler)
		}
		if len(route.Middlewares) > 0 {
			handler = middlewares.ApplyChain(handler, route.Middlewares, registry)
		}

		cleanPath := strings.TrimSuffix(route.Path, "/")
		patterns := []string{cleanPath, cleanPath + "/{rest...}"}

		for _, method := range route.Methods {
			for _, pattern := range patterns {
				mux.Handle(method+" "+pattern, handler)
			}
		}
	}

	return mux
}
