package config

import (
	"fmt"
	"time"

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
	JWTSecret string
	APIKey    string
}

type RouteConfig struct {
	ID          string          `mapstructure:"id"`
	Path        string          `mapstructure:"path"`
	Methods     []string        `mapstructure:"methods"`
	Backends    []BackendConfig `mapstructure:"backends"`
	Middlewares []string        `mapstructure:"middlewares"`
}

type BackendConfig struct {
	URL string `mapstructure:"url"`
}

func LoadConfig(configPath string) (*AppConfig, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("Error while reading config file: %w", err)
	}

	var config AppConfig
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("Error while unmarshaling config: %w", err)
	}

	config.Security.JWTSecret = viper.GetString("JWT_SECRET")
	config.Security.APIKey = viper.GetString("API_KEY")

	if envPort := viper.GetInt("PORT"); envPort != 0 {
		config.Server.Port = envPort
	}

	return &config, nil
}
