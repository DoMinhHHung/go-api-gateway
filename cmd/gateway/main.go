package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/DoMinhHHung/go-api-gateway/internal/core"
)

func main() {
	log.Println("Starting API Gateway...")

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Fatal error: %v", err)
	}

	mux := core.SetupRoutes(cfg)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	log.Printf("API Gateway running at http://localhost%s", addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server crashed: %v", err)
	}
}
