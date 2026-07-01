package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/DoMinhHHung/go-api-gateway/internal/core"
	"github.com/joho/godotenv"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting API Gateway...")

	if err := godotenv.Load(); err != nil {
		slog.Warn("No .env file found, using environment variables")
	}

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Fatal error: %v", err)
	}

	rdb := core.InitRedis(cfg.Security.RedisAddr)
	mux := core.SetupRoutes(cfg, rdb)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	slog.Info("API Gateway running", "url", "http://localhost"+addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server crashed: %v", err)
	}
}
