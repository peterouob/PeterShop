package redis

import (
	"context"
	_ "embed"
	"fmt"
	"math"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/redis/go-redis/v9"
)

//go:embed lua/window.lua
var windowSrc string

//go:embed lua/buy.lua
var buySrc string

var buyScript = redis.NewScript(windowSrc + "\n" + buySrc)

//go:embed lua/release.lua
var releaseSrc string

var releaseScript = redis.NewScript(releaseSrc)

type StockRepo struct {
	redisClient *redis.Client
}

func NewStockRepo(rdb *redis.Client) *StockRepo {
	return &StockRepo{redisClient: rdb}
}

const failedCode = math.MinInt

func (r *StockRepo) DeductStock(ctx context.Context, activityID int64, userID string) error {
	keys := []string{rediskey.Meta(activityID), rediskey.Stock(activityID), rediskey.Users(activityID)}
	code, err := buyScript.Run(ctx, r.redisClient, keys, userID).Int()
	if err != nil {
		return fmt.Errorf("redis script exec failed: %w", err)
	}

	return deductCode(code).Err()
}

func (r *StockRepo) Release(ctx context.Context, activityID int64, userID string) (int, error) {
	keys := []string{rediskey.Stock(activityID), rediskey.Users(activityID)}
	code, err := releaseScript.Run(ctx, r.redisClient, keys, userID).Int()
	if err != nil {
		return failedCode, fmt.Errorf("redis script exec failed: %w", err)
	}

	return code, nil
}
