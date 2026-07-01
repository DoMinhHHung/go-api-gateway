package core

import (
	"context"
	"log"

	"github.com/redis/go-redis/v9"
)

func InitRedis(addr string) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "",
		DB:       0,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Cannot connect to Redis at %s: %v", addr, err)
	}

	log.Printf("Successfully connected to Redis at %s", addr)
	return rdb
}
