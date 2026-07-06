package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/DoMinhHHung/go-api-gateway/internal/middlewares"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker"
)

type rrProxy struct {
	proxies []*httputil.ReverseProxy
	next    uint64
}

func (rr *rrProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if len(rr.proxies) == 0 {
		http.Error(w, "no backend available", http.StatusServiceUnavailable)
		return
	}

	idx := atomic.AddUint64(&rr.next, 1) - 1
	rr.proxies[int(idx%uint64(len(rr.proxies)))].ServeHTTP(w, r)
}

func newReverseProxy(target *url.URL, route config.RouteConfig) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.URL.Path = singleJoiningSlash(target.Path, rewriteRoutePath(route, req.URL.Path))
		},
	}

	baseTransport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		MaxConnsPerHost:       200,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: route.EffectiveResponseHeaderTimeout(),
	}
	proxy.Transport = NewBreakerTransport(baseTransport, route.ID+"|"+target.Host, route.CircuitBreaker)

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		if err == gobreaker.ErrOpenState {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
			w.Write([]byte(`{"error": "Service Unavailable", "message": "Service is currently unavailable, please try again later!"}`))
			return
		}
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) || strings.Contains(err.Error(), "http: request body too large") {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			w.Write([]byte(`{"error": "Request Entity Too Large", "message": "Request body exceeds the allowed limit."}`))
			return
		}
		w.WriteHeader(http.StatusBadGateway) // 502
		w.Write([]byte(`{"error": "Bad Gateway", "message": "Can not connected to service!"}`))
	}

	return proxy
}

func routeHandler(route config.RouteConfig, registry map[string]middlewares.Middleware, rdb *redis.Client, trustProxyHeaders bool, maxBodyBytes int64) http.Handler {
	backends := make([]*httputil.ReverseProxy, 0, len(route.Backends))
	for _, backend := range route.Backends {
		targetURL, err := url.Parse(backend.URL)
		if err != nil {
			continue
		}
		backends = append(backends, newReverseProxy(targetURL, route))
	}

	handler := http.Handler(&rrProxy{proxies: backends})

	handler = middlewares.Recovery()(handler)

	if route.RateLimit.Enabled {
		handler = middlewares.RateLimit(rdb, route.ID, route.RateLimit, trustProxyHeaders)(handler)
	}
	if len(route.Middlewares) > 0 {
		handler = middlewares.ApplyChain(handler, route.Middlewares, registry)
	}
	handler = middlewares.Metrics(route.ID)(handler)
	handler = middlewares.BodyLimit(maxBodyBytes)(handler)
	handler = middlewares.RequestID()(handler)

	return handler
}

func singleJoiningSlash(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	aSlash := strings.HasSuffix(a, "/")
	bSlash := strings.HasPrefix(b, "/")
	switch {
	case aSlash && bSlash:
		return a + b[1:]
	case !aSlash && !bSlash:
		return a + "/" + b
	}
	return a + b
}

func trimRoutePrefix(routePath, requestPath string) string {
	if routePath == "" || routePath == "/" {
		if requestPath == "" {
			return "/"
		}
		return requestPath
	}
	if !strings.HasPrefix(requestPath, routePath) {
		return requestPath
	}
	suffix := strings.TrimPrefix(requestPath, routePath)
	if suffix == "" {
		return "/"
	}
	if !strings.HasPrefix(suffix, "/") {
		return "/" + suffix
	}
	return suffix
}

func rewriteRoutePath(route config.RouteConfig, requestPath string) string {
	suffix := trimRoutePrefix(route.Path, requestPath)
	if route.StripPrefix {
		return suffix
	}
	if route.RewritePrefix != "" {
		return singleJoiningSlash(route.RewritePrefix, strings.TrimPrefix(suffix, "/"))
	}
	return requestPath
}

func routePatterns(path string) []string {
	if path == "/" {
		return []string{"/", "/{rest...}"}
	}
	cleanPath := strings.TrimSuffix(path, "/")
	return []string{cleanPath, cleanPath + "/{rest...}"}
}

func SetupRoutes(cfg *config.AppConfig, rdb *redis.Client) *http.ServeMux {
	mux := http.NewServeMux()
	registry := middlewares.InitRegistry(cfg)
	mux.Handle("GET /metrics", promhttp.Handler())

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

		handler := routeHandler(route, registry, rdb, cfg.Server.TrustProxyHeaders, cfg.Server.MaxBodyBytes)
		patterns := routePatterns(route.Path)

		for _, method := range route.Methods {
			for _, pattern := range patterns {
				mux.Handle(method+" "+pattern, handler)
			}
		}
	}

	return mux
}
