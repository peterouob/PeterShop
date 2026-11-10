package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/service/order-service/internal/repository/postgresql"
	"github.com/peterouob/seckill_service/service/order-service/internal/repository/redis"
)

type OrderRepo interface {
	InsertPendingOrders(ctx context.Context, evs []event.OrderReserved, payTimeout time.Duration) error
	ScheduleExpiry(ctx context.Context, evs []event.OrderReserved, payTimeout time.Duration) error
}

type orderRepo struct {
	*postgresql.PendingRepo
	*redis.TimeoutRepo
}

func NewOrderRepo(rdb *goredis.Client, pool *pgxpool.Pool) OrderRepo {
	return &orderRepo{
		PendingRepo: postgresql.NewPendingRepo(pool),
		TimeoutRepo: redis.NewTimeoutRepo(rdb),
	}
}
