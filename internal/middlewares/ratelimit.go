package middlewares

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/redis/go-redis/v9"
)

var timeNow = time.Now

var tokenBucketScript = redis.NewScript(`
	local key = KEYS[1]
	local rate = tonumber(ARGV[1])
	local capacity = tonumber(ARGV[2])
	local now = tonumber(ARGV[3])
	local requested = 1

	local info = redis.pcall("HMGET", key, "tokens", "last_refreshed")
	local last_tokens = tonumber(info[1])
	local last_refreshed = tonumber(info[2])

	if last_tokens == nil then
		last_tokens = capacity
		last_refreshed = now
	end

	local delta = math.max(0, now - last_refreshed)
	local filled_tokens = math.min(capacity, last_tokens + (delta * rate))

	local allowed = filled_tokens >= requested
	local new_tokens = filled_tokens
	if allowed then
		new_tokens = filled_tokens - requested
	end

	redis.pcall("HMSET", key, "tokens", new_tokens, "last_refreshed", now)
	redis.pcall("EXPIRE", key, math.ceil(capacity/rate) * 2)

	return { allowed and 1 or 0, new_tokens }
`)

func RateLimit(rdb *redis.Client, routeID string, cfg config.RateLimitConfig, trustProxyHeaders bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			identifier := r.Header.Get("X-User-Id")
			if identifier == "" {
				identifier = clientIP(r, trustProxyHeaders)
			}

			key := fmt.Sprintf("ratelimit:%s:%s", routeID, identifier)
			now := timeNow().Unix()

			result, err := tokenBucketScript.Run(context.Background(), rdb, []string{key}, cfg.Rate, cfg.Capacity, now).Result()
			if err != nil {
				if handleRateLimitFailure(w, routeID, cfg.FailClosed, "rate limit check failed", err) {
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			allowed, remaining, ok := parseTokenBucketResult(result)
			if !ok {
				if handleRateLimitFailure(w, routeID, cfg.FailClosed, "rate limit script returned unexpected shape", result) {
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.Capacity))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests) // Error 429
				w.Write([]byte(`{"error": "Too Many Requests", "message": "Too many requests, please try again later."}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func handleRateLimitFailure(w http.ResponseWriter, routeID string, failClosed bool, reason string, details interface{}) bool {
	if !failClosed {
		slog.Warn(reason+", failing open", "route", routeID, "detail", details)
		return false
	}

	slog.Error(reason+", failing closed (blocking request)", "route", routeID, "detail", details)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte(`{"error": "Service Unavailable", "message": "Rate limiter unavailable, request blocked for safety."}`))
	return true
}

func parseTokenBucketResult(result interface{}) (allowed bool, remaining int64, ok bool) {
	resArr, ok := result.([]interface{})
	if !ok || len(resArr) != 2 {
		return false, 0, false
	}
	allowedInt, ok1 := resArr[0].(int64)
	remaining, ok2 := resArr[1].(int64)
	if !ok1 || !ok2 {
		return false, 0, false
	}
	return allowedInt == 1, remaining, true
}

func clientIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
		if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
			return xrip
		}
	}

	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
