package main

import (
	"log"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
)

func main() {
	log.Println("Starting API Gateway...")

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Fatal error: %v", err)
	}

	log.Printf("Server running on port: %d", cfg.Server.Port)
	log.Printf("Read Timeout: %v", cfg.Server.ReadTimeout)
	log.Printf("Write Timeout: %v", cfg.Server.WriteTimeout)

	log.Println("Loaded routes:")
	for _, route := range cfg.Routes {
		log.Printf(" - [%s] Route %s -> Backend %s", route.ID, route.Path, route.Backends[0].URL)
	}
}
