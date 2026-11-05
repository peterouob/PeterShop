package repository

import (
	"context"
	_ "embed"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/redis/go-redis/v9"
)

//go:embed window.lua
var windowSrc string

//go:embed buy.lua
var buySrc string

var buyScript = redis.NewScript(windowSrc + "\n" + buySrc)

//go:embed release.lua
var releaseSrc string

var releaseScript = redis.NewScript(releaseSrc)

type SeckillRepo interface {
	DeductStock(ctx context.Context, activityID int64, userID string) error
}

type seckillRepoImpl struct {
	redisClient *redis.Client
	pool        *pgxpool.Pool
}

func NewSeckillRepo(rdb *redis.Client, pool *pgxpool.Pool) SeckillRepo {
	return &seckillRepoImpl{
		redisClient: rdb,
		pool:        pool,
	}
}

const failedCode = math.MinInt

func (r *seckillRepoImpl) DeductStock(ctx context.Context, activityID int64, userID string) error {
	keys := []string{rediskey.Meta(activityID), rediskey.Stock(activityID), rediskey.Users(activityID)}
	code, err := buyScript.Run(ctx, r.redisClient, keys, userID).Int()
	if err != nil {
		return fmt.Errorf("redis script exec failed: %w", err)
	}

	return deductCode(code).Err()
}

func (r *seckillRepoImpl) Release(ctx context.Context, activityID int64, userID string) (int, error) {
	keys := []string{rediskey.Stock(activityID), rediskey.Users(activityID)}
	code, err := releaseScript.Run(ctx, r.redisClient, keys, userID).Int()
	if err != nil {
		return failedCode, fmt.Errorf("redis script exec failed: %w", err)
	}

	return code, nil
}
