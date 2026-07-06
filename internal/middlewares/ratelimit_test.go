package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRateLimit_Disabled_PassesThrough(t *testing.T) {
	rdb := newTestRedis(t)
	cfg := config.RateLimitConfig{Enabled: false, Rate: 1, Capacity: 1}

	handler := RateLimit(rdb, "route1", cfg, false)(okHandler())

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 when disabled, got %d", i, rec.Code)
		}
	}
}

func TestRateLimit_AllowsBurstUpToCapacity_ThenRejects(t *testing.T) {
	rdb := newTestRedis(t)
	cfg := config.RateLimitConfig{Enabled: true, Rate: 0, Capacity: 3} // rate 0: no refill mid-test

	handler := RateLimit(rdb, "route1", cfg, false)(okHandler())

	fixedNow := time.Unix(1_700_000_000, 0)
	orig := timeNow
	timeNow = func() time.Time { return fixedNow }
	defer func() { timeNow = orig }()

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = "1.2.3.4:5555"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d within capacity: expected 200, got %d", i, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "1.2.3.4:5555"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exhausting capacity, got %d", rec.Code)
	}
}

func TestRateLimit_RefillsOverTime(t *testing.T) {
	rdb := newTestRedis(t)
	cfg := config.RateLimitConfig{Enabled: true, Rate: 1, Capacity: 1} // 1 token/sec, burst 1

	handler := RateLimit(rdb, "route1", cfg, false)(okHandler())

	current := time.Unix(1_700_000_000, 0)
	orig := timeNow
	timeNow = func() time.Time { return current }
	defer func() { timeNow = orig }()

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.RemoteAddr = "9.9.9.9:1"
		return r
	}

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req())
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request should pass, got %d", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req())
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second immediate request should be limited, got %d", rec2.Code)
	}

	current = current.Add(1 * time.Second)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req())
	if rec3.Code != http.StatusOK {
		t.Fatalf("after 1s refill should pass, got %d", rec3.Code)
	}
}

func TestRateLimit_SeparateBucketsPerIdentifier(t *testing.T) {
	rdb := newTestRedis(t)
	cfg := config.RateLimitConfig{Enabled: true, Rate: 0, Capacity: 1}
	handler := RateLimit(rdb, "route1", cfg, false)(okHandler())

	reqA := httptest.NewRequest(http.MethodGet, "/x", nil)
	reqA.RemoteAddr = "1.1.1.1:1"
	recA := httptest.NewRecorder()
	handler.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("user A first request should pass, got %d", recA.Code)
	}

	reqB := httptest.NewRequest(http.MethodGet, "/x", nil)
	reqB.RemoteAddr = "2.2.2.2:1"
	recB := httptest.NewRecorder()
	handler.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("user B is a different bucket, should pass, got %d", recB.Code)
	}
}

func TestRateLimit_PrefersXUserIdOverIP(t *testing.T) {
	rdb := newTestRedis(t)
	cfg := config.RateLimitConfig{Enabled: true, Rate: 0, Capacity: 1}
	handler := RateLimit(rdb, "route1", cfg, false)(okHandler())

	req1 := httptest.NewRequest(http.MethodGet, "/x", nil)
	req1.RemoteAddr = "1.1.1.1:1"
	req1.Header.Set("X-User-Id", "user-42")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	req2.RemoteAddr = "9.9.9.9:1"
	req2.Header.Set("X-User-Id", "user-42")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("same user from different IP must share bucket, expected 429, got %d", rec2.Code)
	}
}

func TestClientIP_IgnoresProxyHeadersWhenNotTrusted(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "10.0.0.5:4444"
	req.Header.Set("X-Forwarded-For", "1.2.3.4") // attacker-supplied, must be ignored

	got := clientIP(req, false)
	if got != "10.0.0.5" {
		t.Fatalf("expected RemoteAddr to win when trustProxyHeaders=false, got %q", got)
	}
}

func TestClientIP_UsesXForwardedForWhenTrusted(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "10.0.0.5:4444"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")

	got := clientIP(req, true)
	if got != "203.0.113.9" {
		t.Fatalf("expected first XFF entry (real client), got %q", got)
	}
}

func TestClientIP_FallsBackToXRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "10.0.0.5:4444"
	req.Header.Set("X-Real-IP", "203.0.113.9")

	got := clientIP(req, true)
	if got != "203.0.113.9" {
		t.Fatalf("expected X-Real-IP, got %q", got)
	}
}

func TestRateLimit_FailsOpenWhenRedisUnavailable(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	cfg := config.RateLimitConfig{Enabled: true, Rate: 1, Capacity: 1}
	handler := RateLimit(rdb, "route1", cfg, false)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected fail-open (200) when Redis is unreachable, got %d", rec.Code)
	}
}

func TestRateLimit_FailsOpenWhenScriptReturnsUnexpectedShape(t *testing.T) {
	rdb := newTestRedis(t)
	cfg := config.RateLimitConfig{Enabled: true, Rate: 1, Capacity: 1}
	handler := RateLimit(rdb, "route1", cfg, false)(okHandler())

	origScript := tokenBucketScript
	tokenBucketScript = redis.NewScript(`return {1}`)
	defer func() {
		tokenBucketScript = origScript
	}()

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "4.4.4.4:1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected fail-open (200) for malformed script result, got %d", rec.Code)
	}
}

func TestRateLimit_FailsClosedWhenRedisUnavailable(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	cfg := config.RateLimitConfig{Enabled: true, Rate: 1, Capacity: 1, FailClosed: true}
	handler := RateLimit(rdb, "route-auth", cfg, false)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected fail-closed (503) when Redis is unreachable, got %d", rec.Code)
	}
}

func TestRateLimit_FailsClosedWhenScriptReturnsUnexpectedShape(t *testing.T) {
	rdb := newTestRedis(t)
	cfg := config.RateLimitConfig{Enabled: true, Rate: 1, Capacity: 1, FailClosed: true}
	handler := RateLimit(rdb, "route-auth", cfg, false)(okHandler())

	origScript := tokenBucketScript
	tokenBucketScript = redis.NewScript(`return {1}`)
	defer func() {
		tokenBucketScript = origScript
	}()

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "5.5.5.5:1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected fail-closed (503) for malformed script result, got %d", rec.Code)
	}
}

func TestParseTokenBucketResult(t *testing.T) {
	cases := []struct {
		name        string
		input       interface{}
		wantOK      bool
		wantAllowed bool
		wantRemain  int64
	}{
		{"valid allowed", []interface{}{int64(1), int64(4)}, true, true, 4},
		{"valid denied", []interface{}{int64(0), int64(0)}, true, false, 0},
		{"wrong type", "not an array", false, false, 0},
		{"wrong length", []interface{}{int64(1)}, false, false, 0},
		{"non-int element", []interface{}{"1", int64(4)}, false, false, 0},
		{"nil", nil, false, false, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, remaining, ok := parseTokenBucketResult(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && (allowed != tc.wantAllowed || remaining != tc.wantRemain) {
				t.Fatalf("got allowed=%v remaining=%d, want allowed=%v remaining=%d",
					allowed, remaining, tc.wantAllowed, tc.wantRemain)
			}
		})
	}
}
