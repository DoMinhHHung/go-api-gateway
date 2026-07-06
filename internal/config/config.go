package config

import (
	"crypto/rsa"
	"fmt"
	"net/url"
	"os"
	"strings"
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
	MaxBodyBytes      int64         `mapstructure:"max_body_bytes"`
}

type SecurityConfig struct {
	JWTPublicKey  *rsa.PublicKey
	JWTAudience   string
	JWTIssuer     string
	APIKey        string
	RedisAddr     string
	RedisPassword string
	RedisTLS      bool
}

type RateLimitConfig struct {
	Enabled    bool    `mapstructure:"enabled"`
	Rate       float64 `mapstructure:"rate"`
	Capacity   int     `mapstructure:"capacity"`
	FailClosed bool    `mapstructure:"fail_closed"`
}

type TimeoutConfig struct {
	ResponseHeader time.Duration `mapstructure:"response_header"`
}

type RouteConfig struct {
	ID             string               `mapstructure:"id"`
	Path           string               `mapstructure:"path"`
	Methods        []string             `mapstructure:"methods"`
	Backends       []BackendConfig      `mapstructure:"backends"`
	Middlewares    []string             `mapstructure:"middlewares"`
	StripPrefix    bool                 `mapstructure:"strip_prefix"`
	RewritePrefix  string               `mapstructure:"rewrite_prefix"`
	RateLimit      RateLimitConfig      `mapstructure:"rate_limit"`
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	Timeout        TimeoutConfig        `mapstructure:"timeout"`
}

const DefaultResponseHeaderTimeout = 5 * time.Second
const DefaultMaxBodyBytes int64 = 1 << 20

func (r RouteConfig) EffectiveResponseHeaderTimeout() time.Duration {
	if r.Timeout.ResponseHeader <= 0 {
		return DefaultResponseHeaderTimeout
	}
	return r.Timeout.ResponseHeader
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

	if cfg.Server.MaxBodyBytes <= 0 {
		cfg.Server.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if envMaxBodyBytes := v.GetInt64("MAX_BODY_BYTES"); envMaxBodyBytes > 0 {
		cfg.Server.MaxBodyBytes = envMaxBodyBytes
	}

	cfg.Security.APIKey = v.GetString("API_KEY")
	if cfg.Security.APIKey == "" {
		return nil, fmt.Errorf("API_KEY is not set — refusing to start with API key auth disabled")
	}
	cfg.Security.JWTAudience = v.GetString("JWT_AUDIENCE")
	if cfg.Security.JWTAudience == "" {
		return nil, fmt.Errorf("JWT_AUDIENCE is not set")
	}
	cfg.Security.JWTIssuer = v.GetString("JWT_ISSUER")
	if cfg.Security.JWTIssuer == "" {
		return nil, fmt.Errorf("JWT_ISSUER is not set")
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
	if v.Get("TRUST_PROXY_HEADERS") != nil {
		cfg.Server.TrustProxyHeaders = v.GetBool("TRUST_PROXY_HEADERS")
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
		if !strings.HasPrefix(route.Path, "/") {
			return fmt.Errorf("route %q: path must start with /", route.ID)
		}
		if route.RewritePrefix != "" && !strings.HasPrefix(route.RewritePrefix, "/") {
			return fmt.Errorf("route %q: rewrite_prefix must start with /", route.ID)
		}
		if route.StripPrefix && route.RewritePrefix != "" {
			return fmt.Errorf("route %q: strip_prefix and rewrite_prefix cannot both be set", route.ID)
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
