package core

import (
	"context"
	"testing"

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