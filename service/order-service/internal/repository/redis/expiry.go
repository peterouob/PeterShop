package redis

import (
	"context"
	"strconv"
	"time"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/redis/go-redis/v9"
)

type TimeoutRepo struct {
	rdb *redis.Client
}

func NewTimeoutRepo(rdb *redis.Client) *TimeoutRepo {
	return &TimeoutRepo{rdb: rdb}
}

func (r *TimeoutRepo) ScheduleExpiry(ctx context.Context, evs []event.OrderReserved, payTimeout time.Duration) error {
	if len(evs) == 0 {
		return nil
	}

	zs := make([]redis.Z, 0, len(evs))
	for _, ev := range evs {
		zs = append(zs, redis.Z{
			Score:  float64(ev.ExpireAtMs(payTimeout)),
			Member: strconv.FormatInt(ev.OrderNo, 10),
		})
	}

	return r.rdb.ZAdd(ctx, rediskey.OrderExpiryQueue, zs...).Err()
}
