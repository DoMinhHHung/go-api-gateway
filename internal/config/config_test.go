package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func copyPublicKey(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "jwt_public.pem")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

func generateTestRSAPublicKeyPEM(t *testing.T) string {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	return copyPublicKey(t, string(pemBytes))
}

func testConfigYAML() string {
	return `server:
  port: 1234
  read_timeout: 2s
  write_timeout: 3s
  trust_proxy_headers: true
routes:
  - id: route-1
    path: /api
    methods: [GET, POST]
    backends:
      - url: http://example.com
`
}

func TestLoadConfig_Success(t *testing.T) {
	configPath := writeTempConfig(t, testConfigYAML())
	t.Setenv("API_KEY", "test-api-key")
	t.Setenv("JWT_PUBLIC_KEY_PATH", generateTestRSAPublicKeyPEM(t))
	t.Setenv("REDIS_ADDR", "redis.example:6380")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_TLS_ENABLED", "true")
	t.Setenv("PORT", "9090")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Fatalf("expected env port override 9090, got %d", cfg.Server.Port)
	}
	if !cfg.Server.TrustProxyHeaders {
		t.Fatal("expected trust proxy headers to be loaded")
	}
	if cfg.Security.APIKey != "test-api-key" {
		t.Fatalf("expected API key to load, got %q", cfg.Security.APIKey)
	}
	if cfg.Security.RedisAddr != "redis.example:6380" || cfg.Security.RedisPassword != "secret" || !cfg.Security.RedisTLS {
		t.Fatalf("unexpected redis config: %+v", cfg.Security)
	}
	if cfg.Security.JWTPublicKey == nil {
		t.Fatal("expected JWT public key to be loaded")
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0].ID != "route-1" {
		t.Fatalf("unexpected routes: %+v", cfg.Routes)
	}
}

func TestLoadConfig_ErrorsWhenAPIKeyMissing(t *testing.T) {
	configPath := writeTempConfig(t, testConfigYAML())
	t.Setenv("API_KEY", "")
	t.Setenv("JWT_PUBLIC_KEY_PATH", generateTestRSAPublicKeyPEM(t))

	_, err := LoadConfig(configPath)
	if err == nil || !strings.Contains(err.Error(), "API_KEY") {
		t.Fatalf("expected API_KEY error, got %v", err)
	}
}

func TestLoadConfig_ErrorsWhenJWTPublicKeyPathMissing(t *testing.T) {
	configPath := writeTempConfig(t, testConfigYAML())
	t.Setenv("API_KEY", "test-api-key")
	t.Setenv("JWT_PUBLIC_KEY_PATH", "")

	_, err := LoadConfig(configPath)
	if err == nil || !strings.Contains(err.Error(), "JWT_PUBLIC_KEY_PATH") {
		t.Fatalf("expected JWT_PUBLIC_KEY_PATH error, got %v", err)
	}
}

func TestLoadConfig_ErrorsWhenJWTPublicKeyInvalid(t *testing.T) {
	configPath := writeTempConfig(t, testConfigYAML())
	keyPath := copyPublicKey(t, "not a pem file")
	t.Setenv("API_KEY", "test-api-key")
	t.Setenv("JWT_PUBLIC_KEY_PATH", keyPath)

	_, err := LoadConfig(configPath)
	if err == nil || !strings.Contains(err.Error(), "invalid JWT public key format") {
		t.Fatalf("expected invalid key error, got %v", err)
	}
}

func TestValidateRoutes(t *testing.T) {
	tests := []struct {
		name    string
		routes  []RouteConfig
		wantErr string
	}{
		{
			name: "valid",
			routes: []RouteConfig{{
				ID:      "route-1",
				Path:    "/api",
				Methods: []string{"GET"},
				Backends: []BackendConfig{{
					URL: "http://example.com",
				}},
			}},
		},
		{
			name:    "empty id",
			routes:  []RouteConfig{{Path: "/api", Methods: []string{"GET"}, Backends: []BackendConfig{{URL: "http://example.com"}}}},
			wantErr: "empty id",
		},
		{
			name: "duplicate id",
			routes: []RouteConfig{{ID: "route-1", Path: "/api", Methods: []string{"GET"}, Backends: []BackendConfig{{URL: "http://example.com"}}},
				{ID: "route-1", Path: "/api-2", Methods: []string{"GET"}, Backends: []BackendConfig{{URL: "http://example.com"}}}},
			wantErr: "duplicate route id",
		},
		{
			name:    "missing path",
			routes:  []RouteConfig{{ID: "route-1", Methods: []string{"GET"}, Backends: []BackendConfig{{URL: "http://example.com"}}}},
			wantErr: "path is required",
		},
		{
			name:    "missing methods",
			routes:  []RouteConfig{{ID: "route-1", Path: "/api", Backends: []BackendConfig{{URL: "http://example.com"}}}},
			wantErr: "at least one method is required",
		},
		{
			name:    "missing backend",
			routes:  []RouteConfig{{ID: "route-1", Path: "/api", Methods: []string{"GET"}}},
			wantErr: "at least one backend is required",
		},
		{
			name:    "invalid backend url",
			routes:  []RouteConfig{{ID: "route-1", Path: "/api", Methods: []string{"GET"}, Backends: []BackendConfig{{URL: "not-a-url"}}}},
			wantErr: "invalid backend url",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRoutes(tc.routes)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q error, got %v", tc.wantErr, err)
			}
		})
	}
}
