package repository

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
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
	DeductStock(ctx context.Context, activityID int64, userID string) (int, error)
}

type seckillRepoImpl struct {
	redisClient *redis.Client
	db          *gorm.DB
}

func NewSeckillRepo(rdb *redis.Client, db *gorm.DB) SeckillRepo {
	return &seckillRepoImpl{
		redisClient: rdb,
		db:          db,
	}
}

var seckillScript = redis.NewScript(`
    local stock_key = KEYS[1]
    local users_key = KEYS[2]
    
    local user_id = ARGV[1]

    local user_exist = redis.call('sismember', users_key, user_id)
    if tonumber(user_exist) == 1 then
        return 2
    end

    local stock_val = redis.call('get', stock_key)
    if not stock_val then
        return -1
    end

    if tonumber(stock_val) <= 0 then
        return 0
    else
        redis.call('decr', stock_key)
        redis.call('sadd', users_key, user_id)
        return 1
    end
`)

const failedCode = math.MinInt

func (r *seckillRepoImpl) Deduct(ctx context.Context, activityID int64, userID string) (int, error) {
	keys := []string{rediskey.Meta(activityID), rediskey.Stock(activityID), rediskey.Users(activityID)}
	code, err := buyScript.Run(ctx, r.redisClient, keys, userID).Int()
	if err != nil {
		return failedCode, fmt.Errorf("redis script exec failed: %w", err)
	}

	return code, nil
}

func (r *seckillRepoImpl) Release(ctx context.Context, activityID int64, userID string) (int, error) {
	keys := []string{rediskey.Stock(activityID), rediskey.Users(activityID)}
	code, err := releaseScript.Run(ctx, r.redisClient, keys, userID).Int()
	if err != nil {
		return failedCode, fmt.Errorf("redis script exec failed: %w", err)
	}

	return code, nil
}

func (r *seckillRepoImpl) DeductStock(ctx context.Context, activityID int64, userID string) (int, error) {
	keys := []string{rediskey.Stock(activityID), rediskey.Users(activityID)}
	result, err := seckillScript.Run(ctx, r.redisClient, keys, userID).Result()
	if err != nil {
		return 0, fmt.Errorf("redis script exec failed: %w", err)
	}

	v, ok := result.(int64)
	if !ok {
		return 0, errors.New("DeductStock failed: result is not int64")
	}
	return int(v), nil
}
