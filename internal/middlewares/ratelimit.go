package middlewares

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/redis/go-redis/v9"
)

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
	-- Set TTL để tự xóa rác trong Redis nếu key không xài nữa
	redis.pcall("EXPIRE", key, math.ceil(capacity/rate) * 2)

	return { allowed and 1 or 0, new_tokens }
`)

func RateLimit(rdb *redis.Client, routeID string, cfg config.RateLimitConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			identifier := r.Header.Get("X-User-Id")
			if identifier == "" {
				ip := r.RemoteAddr
				if colonIdx := strings.LastIndex(ip, ":"); colonIdx != -1 {
					ip = ip[:colonIdx]
				}
				identifier = ip
			}

			key := fmt.Sprintf("ratelimit:%s:%s", routeID, identifier)
			now := time.Now().Unix()

			result, err := tokenBucketScript.Run(context.Background(), rdb, []string{key}, cfg.Rate, cfg.Capacity, now).Result()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			resArr := result.([]interface{})
			allowed := resArr[0].(int64) == 1
			remaining := resArr[1].(int64)

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
