package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type AppConfig struct {
	Server ServerConfig  `mapstructure:"server"`
	Routes []RouteConfig `mapstructure:"routes"`
}

type ServerConfig struct {
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type RouteConfig struct {
	ID       string          `mapstructure:"id"`
	Path     string          `mapstructure:"path"`
	Methods  []string        `mapstructure:"methods"`
	Backends []BackendConfig `mapstructure:"backends"`
}

type BackendConfig struct {
	URL string `mapstructure:"url"`
}

func LoadConfig(configPath string) (*AppConfig, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("lỗi khi đọc file config: %w", err)
	}

	var config AppConfig
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("lỗi khi map config vào struct: %w", err)
	}

	return &config, nil
}
