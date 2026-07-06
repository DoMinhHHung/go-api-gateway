package core

import (
	"context"
	"crypto/tls"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisConnectAttempts  = 6
	defaultRedisConnectBaseDelay = 2 * time.Second
)

func InitRedis(addr, password string, useTLS bool) *redis.Client {
	return initRedisWithRetry(addr, password, useTLS, defaultRedisConnectAttempts, defaultRedisConnectBaseDelay)
}

func initRedisWithRetry(addr, password string, useTLS bool, maxAttempts int, baseDelay time.Duration) *redis.Client {
	opts := &redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	}
	if useTLS {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	rdb := redis.NewClient(opts)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := rdb.Ping(ctx).Err()
		cancel()

		if err == nil {
			slog.Info("connected to redis", "addr", addr, "tls", useTLS, "attempt", attempt)
			return rdb
		}

		lastErr = err
		if attempt < maxAttempts {
			delay := baseDelay * time.Duration(attempt) // backoff tuyến tính: 2s, 4s, 6s, 8s, 10s...
			slog.Warn("redis not ready yet, retrying",
				"addr", addr, "attempt", attempt, "max_attempts", maxAttempts,
				"retry_in", delay.String(), "err", err)
			time.Sleep(delay)
		}
	}

	slog.Error("cannot connect to redis after retries, giving up", "addr", addr, "attempts", maxAttempts, "err", lastErr)
	os.Exit(1)
	return nil
}
