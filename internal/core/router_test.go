package core

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker"
)

func TestNewReverseProxy_DirectsRequestAndHandlesErrors(t *testing.T) {
	target, err := url.Parse("http://backend.example.local")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}

	proxy := newReverseProxy(target, "route-1", config.CircuitBreakerConfig{Enabled: false})
	req := httptest.NewRequest(http.MethodGet, "http://original.local/api?q=1", nil)
	proxy.Director(req)

	if req.URL.Scheme != "http" || req.URL.Host != "backend.example.local" {
		t.Fatalf("unexpected directed url: %s://%s", req.URL.Scheme, req.URL.Host)
	}
	if req.Host != "backend.example.local" {
		t.Fatalf("expected host rewrite, got %q", req.Host)
	}

	openRec := httptest.NewRecorder()
	proxy.ErrorHandler(openRec, req, gobreaker.ErrOpenState)
	if openRec.Code != http.StatusServiceUnavailable || !strings.Contains(openRec.Body.String(), "Service Unavailable") {
		t.Fatalf("expected 503 service unavailable, got %d body=%s", openRec.Code, openRec.Body.String())
	}

	badGatewayRec := httptest.NewRecorder()
	proxy.ErrorHandler(badGatewayRec, req, errors.New("boom"))
	if badGatewayRec.Code != http.StatusBadGateway || !strings.Contains(badGatewayRec.Body.String(), "Bad Gateway") {
		t.Fatalf("expected 502 bad gateway, got %d body=%s", badGatewayRec.Code, badGatewayRec.Body.String())
	}
}

func TestSetupRoutes_HealthReadyAndMiddlewareProtectedRoute(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	var backendHits int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backendHits, 1)
		if r.Header.Get("X-API-Key") != "test-api-key" {
			t.Fatalf("expected api key header to reach backend, got %q", r.Header.Get("X-API-Key"))
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	t.Cleanup(backend.Close)

	cfg := &config.AppConfig{
		Server: config.ServerConfig{},
		Security: config.SecurityConfig{
			APIKey: "test-api-key",
		},
		Routes: []config.RouteConfig{{
			ID:          "route-1",
			Path:        "/api",
			Methods:     []string{http.MethodGet},
			Backends:    []config.BackendConfig{{URL: backend.URL}},
			Middlewares: []string{"api_key"},
			RateLimit:   config.RateLimitConfig{Enabled: true, Rate: 0, Capacity: 1},
		}},
	}

	mux := SetupRoutes(cfg, rdb)

	healthRec := httptest.NewRecorder()
	mux.ServeHTTP(healthRec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if healthRec.Code != http.StatusOK || !strings.Contains(healthRec.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected healthz response: code=%d body=%s", healthRec.Code, healthRec.Body.String())
	}

	readyRec := httptest.NewRecorder()
	mux.ServeHTTP(readyRec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if readyRec.Code != http.StatusOK || !strings.Contains(readyRec.Body.String(), `"status":"ready"`) {
		t.Fatalf("unexpected readyz response: code=%d body=%s", readyRec.Code, readyRec.Body.String())
	}

	allowedReq := httptest.NewRequest(http.MethodGet, "/api", nil)
	allowedReq.RemoteAddr = "1.2.3.4:1234"
	allowedReq.Header.Set("X-API-Key", "test-api-key")
	allowedRec := httptest.NewRecorder()
	mux.ServeHTTP(allowedRec, allowedReq)
	if allowedRec.Code != http.StatusCreated || allowedRec.Body.String() != "/api" {
		t.Fatalf("unexpected proxied response: code=%d body=%q", allowedRec.Code, allowedRec.Body.String())
	}

	blockedReq := httptest.NewRequest(http.MethodGet, "/api", nil)
	blockedReq.RemoteAddr = "1.2.3.4:1234"
	blockedReq.Header.Set("X-API-Key", "test-api-key")
	blockedRec := httptest.NewRecorder()
	mux.ServeHTTP(blockedRec, blockedReq)
	if blockedRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate limited, got %d", blockedRec.Code)
	}

	if got := atomic.LoadInt32(&backendHits); got != 1 {
		t.Fatalf("expected backend to be hit once, got %d", got)
	}
}