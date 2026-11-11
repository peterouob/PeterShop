package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/postgresql"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/redis"
)

type SeckillRepo interface {
	DeductStock(ctx context.Context, activityID int64, userID string) (int32, error)
	Reserve(ctx context.Context, orderNo, activityID int64, userID string, slotNo int32) error
	Release(ctx context.Context, aid int64, slotNo int32, userID string, removed bool) error
}

type seckillRepo struct {
	*redis.StockRepo
	*postgresql.ReserveRepo
}

func NewSeckillRepo(rdb *goredis.Client, pool *pgxpool.Pool) SeckillRepo {
	return &seckillRepo{
		StockRepo:   redis.NewStockRepo(rdb),
		ReserveRepo: postgresql.NewReserveRepo(pool),
	}
}
