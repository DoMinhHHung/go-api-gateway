package middlewares

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
)

func TestInitRegistry_ProvidesExpectedMiddlewares(t *testing.T) {
	_, publicKey := genKeyPair(t)
	cfg := &config.AppConfig{
		Security: config.SecurityConfig{
			APIKey:       "test-api-key",
			JWTPublicKey: publicKey,
		},
	}

	registry := InitRegistry(cfg)

	for _, name := range []string{"api_key", "jwt"} {
		if _, ok := registry[name]; !ok {
			t.Fatalf("expected middleware %q to exist", name)
		}
	}

	if _, ok := registry["logging"]; ok {
		t.Fatal("logging must not be an opt-in registry middleware; it is applied unconditionally in routeHandler")
	}
}

func TestApplyChain_ComposesMiddlewareInOrder(t *testing.T) {
	hits := make([]string, 0, 3)
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "handler")
		w.WriteHeader(http.StatusOK)
	})

	registry := map[string]Middleware{
		"first": func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits = append(hits, "first")
				next.ServeHTTP(w, r)
			})
		},
		"second": func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits = append(hits, "second")
				next.ServeHTTP(w, r)
			})
		},
	}

	handler := ApplyChain(base, []string{"first", "missing", "second"}, registry)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !reflect.DeepEqual(hits, []string{"first", "second", "handler"}) {
		t.Fatalf("unexpected execution order: %v", hits)
	}
}
