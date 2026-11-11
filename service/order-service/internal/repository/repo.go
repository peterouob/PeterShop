package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/service/order-service/internal/repository/postgresql"
)

type OrderRepo interface {
	InsertPendingOrders(ctx context.Context, evs []event.OrderReserved, payTimeout time.Duration) error
}

type orderRepo struct {
	*postgresql.PendingRepo
}

func NewOrderRepo(pool *pgxpool.Pool) OrderRepo {
	return &orderRepo{PendingRepo: postgresql.NewPendingRepo(pool)}
}
