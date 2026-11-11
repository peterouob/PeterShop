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

func (r *StockRepo) DeductStock(ctx context.Context, activityID int64, userID string) (int32, error) {
	keys := []string{rediskey.Meta(activityID), rediskey.Slots(activityID), rediskey.Users(activityID)}
	res, err := buyScript.Run(ctx, r.redisClient, keys, userID).Int64Slice()
	if err != nil {
		return 0, fmt.Errorf("redis script exec failed: %w", err)
	}

	if len(res) != 2 {
		return 0, fmt.Errorf("unexpect return length, %d", len(res))
	}

	if res[0] != deductPass {
		return 0, deductCode(res[0]).Err()
	}

	if res[1] <= 0 || res[1] > math.MaxInt32 {
		return 0, fmt.Errorf("invalid slot %d", res[1])
	}

	return int32(res[1]), nil
}

func (r *StockRepo) Release(ctx context.Context, aid int64, slotNo int32, userID string, removed bool) error {
	keys := []string{rediskey.Slots(aid), rediskey.Users(aid), rediskey.Meta(aid)}
	code, err := releaseScript.Run(ctx, r.redisClient, keys, slotNo, userID, removed).Int()
	if err != nil {
		return fmt.Errorf("redis script exec failed: %w", err)
	}

	if code == deductStockLost {
		return ErrStockLost
	}

	return nil
}
