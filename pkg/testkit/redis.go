package testkit

import (
	"context"
	"fmt"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func OpenRedis(t testing.TB) *redis.Client {
	t.Helper()
	ctx := context.Background()

	ctr, err := testcontainers.Run(ctx, "redis:8-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("testkit: start redis container: %v", err)
	}

	addr, err := ctr.PortEndpoint(ctx, fmt.Sprintf("%d/tcp", 6379), "")
	if err != nil {
		t.Fatalf("testkit: get redis port: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("testkit: ping redis: %v", err)
	}
	return rdb
}
