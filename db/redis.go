package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func NewRedisClient(ctx context.Context, addr string) (*redis.Client, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, errors.New("redis address is empty")
	}

	client := redis.NewClient(&redis.Options{Addr: addr})

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}
