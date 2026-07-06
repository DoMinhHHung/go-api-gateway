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
	"time"

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

	route := config.RouteConfig{ID: "route-1", Path: "/api", CircuitBreaker: config.CircuitBreakerConfig{Enabled: false}}
	proxy := newReverseProxy(target, route)
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

func TestNewReverseProxy_PerRouteResponseHeaderTimeout(t *testing.T) {
	slowBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(slowBackend.Close)

	target, err := url.Parse(slowBackend.URL)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}

	t.Run("short timeout triggers bad gateway before backend responds", func(t *testing.T) {
		proxy := newReverseProxy(target, config.RouteConfig{ID: "route-fast-timeout", Path: "/x", CircuitBreaker: config.CircuitBreakerConfig{Enabled: false}, Timeout: config.TimeoutConfig{ResponseHeader: 50 * time.Millisecond}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		proxy.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadGateway {
			t.Fatalf("expected 502 due to short response header timeout, got %d", rec.Code)
		}
	})

	t.Run("long timeout allows slow backend to complete", func(t *testing.T) {
		proxy := newReverseProxy(target, config.RouteConfig{ID: "route-slow-timeout", Path: "/x", CircuitBreaker: config.CircuitBreakerConfig{Enabled: false}, Timeout: config.TimeoutConfig{ResponseHeader: 2 * time.Second}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		proxy.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 when timeout is long enough, got %d", rec.Code)
		}
	})
}

func TestSetupRoutes_RoundRobinAndPathRewrite(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	var firstHits int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&firstHits, 1)
		if got := r.Header.Get("X-Request-Id"); got == "" {
			t.Fatal("expected request id to reach backend")
		}
		_, _ = io.WriteString(w, "first:"+r.URL.Path)
	}))
	t.Cleanup(first.Close)

	var secondHits int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondHits, 1)
		_, _ = io.WriteString(w, "second:"+r.URL.Path)
	}))
	t.Cleanup(second.Close)

	cfg := &config.AppConfig{
		Server:   config.ServerConfig{MaxBodyBytes: config.DefaultMaxBodyBytes},
		Security: config.SecurityConfig{APIKey: "test-api-key", JWTAudience: "gateway-api", JWTIssuer: "gateway-api"},
		Routes: []config.RouteConfig{{
			ID:          "round-robin",
			Path:        "/api/v1/users",
			Methods:     []string{http.MethodGet},
			Backends:    []config.BackendConfig{{URL: first.URL}, {URL: second.URL}},
			Middlewares: []string{"logging"},
			StripPrefix: true,
		}},
	}

	mux := SetupRoutes(cfg, rdb)

	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d body=%s", i, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "/42") {
			t.Fatalf("expected stripped path to reach backend, got %q", rec.Body.String())
		}
		if got := rec.Header().Get("X-Request-Id"); got == "" {
			t.Fatal("expected request id response header")
		}
	}

	if got := atomic.LoadInt32(&firstHits); got != 2 {
		t.Fatalf("expected first backend to receive 2 requests, got %d", got)
	}
	if got := atomic.LoadInt32(&secondHits); got != 2 {
		t.Fatalf("expected second backend to receive 2 requests, got %d", got)
	}
}

func TestSetupRoutes_BodyLimitAndMetrics(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	cfg := &config.AppConfig{
		Server:   config.ServerConfig{MaxBodyBytes: 4},
		Security: config.SecurityConfig{APIKey: "test-api-key", JWTAudience: "gateway-api", JWTIssuer: "gateway-api"},
		Routes: []config.RouteConfig{{
			ID:          "body-limit",
			Path:        "/upload",
			Methods:     []string{http.MethodPost},
			Backends:    []config.BackendConfig{{URL: backend.URL}},
			Middlewares: []string{"logging"},
		}},
	}

	mux := SetupRoutes(cfg, rdb)

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("too-large-body"))
	req.Header.Set("X-API-Key", "test-api-key")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 when body exceeds limit, got %d body=%s", rec.Code, rec.Body.String())
	}

	metricsRec := httptest.NewRecorder()
	mux.ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsRec.Code != http.StatusOK || !strings.Contains(metricsRec.Body.String(), "gateway_http_requests_total") {
		t.Fatalf("expected metrics endpoint output, got code=%d body=%s", metricsRec.Code, metricsRec.Body.String())
	}
}
