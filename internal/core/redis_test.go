package core

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestInitRedis_ConnectsAndPings(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := InitRedis(mr.Addr(), "", false)
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	if rdb.Options().TLSConfig != nil {
		t.Fatal("expected non-TLS client to have nil TLS config")
	}
	if got := rdb.Options().Addr; got != mr.Addr() {
		t.Fatalf("expected addr %q, got %q", mr.Addr(), got)
	}
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("expected redis client to ping, got %v", err)
	}
}

func TestInitRedisWithRetry_RecoversAfterInitialFailures(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	mr := miniredis.NewMiniRedis()
	t.Cleanup(mr.Close)

	go func() {
		time.Sleep(250 * time.Millisecond)
		_ = mr.StartAddr(addr)
	}()

	start := time.Now()
	rdb := initRedisWithRetry(addr, "", false, 5, 150*time.Millisecond)
	t.Cleanup(func() { _ = rdb.Close() })

	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("expected retry to wait for redis to come up, only took %s", elapsed)
	}
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("expected client to work after redis recovered, got %v", err)
	}
}
