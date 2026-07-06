package config

import (
	"crypto/rsa"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
)

type AppConfig struct {
	Server   ServerConfig `mapstructure:"server"`
	Security SecurityConfig
	Routes   []RouteConfig `mapstructure:"routes"`
}

type ServerConfig struct {
	Port              int           `mapstructure:"port"`
	ReadTimeout       time.Duration `mapstructure:"read_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"`
	TrustProxyHeaders bool          `mapstructure:"trust_proxy_headers"`
}

type SecurityConfig struct {
	JWTPublicKey  *rsa.PublicKey
	APIKey        string
	RedisAddr     string
	RedisPassword string
	RedisTLS      bool
}

type RateLimitConfig struct {
	Enabled  bool    `mapstructure:"enabled"`
	Rate     float64 `mapstructure:"rate"`
	Capacity int     `mapstructure:"capacity"`
}

type RouteConfig struct {
	ID             string               `mapstructure:"id"`
	Path           string               `mapstructure:"path"`
	Methods        []string             `mapstructure:"methods"`
	Backends       []BackendConfig      `mapstructure:"backends"`
	Middlewares    []string             `mapstructure:"middlewares"`
	RateLimit      RateLimitConfig      `mapstructure:"rate_limit"`
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}

type BackendConfig struct {
	URL string `mapstructure:"url"`
}

type CircuitBreakerConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	MaxRequests uint32 `mapstructure:"max_requests"`
	Interval    int    `mapstructure:"interval"`
	Timeout     int    `mapstructure:"timeout"`
}

func LoadConfig(configPath string) (*AppConfig, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var cfg AppConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	cfg.Security.APIKey = v.GetString("API_KEY")
	if cfg.Security.APIKey == "" {
		return nil, fmt.Errorf("API_KEY is not set — refusing to start with API key auth disabled")
	}

	pubKeyPath := v.GetString("JWT_PUBLIC_KEY_PATH")
	if pubKeyPath == "" {
		return nil, fmt.Errorf("JWT_PUBLIC_KEY_PATH is not set")
	}
	pubKeyBytes, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read JWT public key at %s: %w", pubKeyPath, err)
	}
	pubKey, err := jwt.ParseRSAPublicKeyFromPEM(pubKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid JWT public key format: %w", err)
	}
	cfg.Security.JWTPublicKey = pubKey

	// --- Redis ---
	redisAddr := v.GetString("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	cfg.Security.RedisAddr = redisAddr
	cfg.Security.RedisPassword = v.GetString("REDIS_PASSWORD")
	cfg.Security.RedisTLS = v.GetBool("REDIS_TLS_ENABLED")

	if envPort := v.GetInt("PORT"); envPort != 0 {
		cfg.Server.Port = envPort
	}

	if err := validateRoutes(cfg.Routes); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateRoutes(routes []RouteConfig) error {
	seen := make(map[string]bool, len(routes))
	for _, route := range routes {
		if route.ID == "" {
			return fmt.Errorf("route config has empty id")
		}
		if seen[route.ID] {
			return fmt.Errorf("duplicate route id %q", route.ID)
		}
		seen[route.ID] = true

		if route.Path == "" {
			return fmt.Errorf("route %q: path is required", route.ID)
		}
		if len(route.Methods) == 0 {
			return fmt.Errorf("route %q: at least one method is required", route.ID)
		}
		if len(route.Backends) == 0 {
			return fmt.Errorf("route %q: at least one backend is required", route.ID)
		}
		if len(route.Backends) > 1 {
			slog.Warn("route declares multiple backends but load balancing is not implemented; only the first backend will be used", "route", route.ID)
		}
		for _, b := range route.Backends {
			u, err := url.Parse(b.URL)
			if err != nil || u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("route %q: invalid backend url %q", route.ID, b.URL)
			}
		}
	}
	return nil
}
