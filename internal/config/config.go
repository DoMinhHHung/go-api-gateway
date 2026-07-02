package config

import (
	"crypto/rsa"
	"fmt"
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
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type SecurityConfig struct {
	JWTPublicKey *rsa.PublicKey
	APIKey       string
	RedisAddr    string
}

type RateLimitConfig struct {
	Enabled  bool `mapstructure:"enabled"`
	Rate     int  `mapstructure:"rate"`
	Capacity int  `mapstructure:"capacity"`
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
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var cfg AppConfig
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	cfg.Security.APIKey = viper.GetString("API_KEY")
	if cfg.Security.APIKey == "" {
		return nil, fmt.Errorf("API_KEY is not set — refusing to start with API key auth disabled")
	}

	pubKeyPath := viper.GetString("JWT_PUBLIC_KEY_PATH")
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
	redisAddr := viper.GetString("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	cfg.Security.RedisAddr = redisAddr

	if envPort := viper.GetInt("PORT"); envPort != 0 {
		cfg.Server.Port = envPort
	}

	return &cfg, nil
}
