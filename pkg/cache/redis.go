package cache

import (
	"context"
	"fmt"

	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
)

func open(cfg *config.Config) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})

	return client
}

var Module = fx.Module("redis",
	fx.Provide(func(lc fx.Lifecycle, cfg *config.Config) (*redis.Client, error) {
		client := open(cfg)
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				if err := client.Ping(ctx).Err(); err != nil {
					return fmt.Errorf("redis: ping: %w", err)
				}
				logger.Log("redis: connect")
				return nil
			},
			OnStop: func(ctx context.Context) error {
				logger.Log("redis: disconnect")
				return client.Close()
			},
		})
		return client, nil
	}),
)
