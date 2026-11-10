package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/postgresql"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/redis"
	goredis "github.com/redis/go-redis/v9"
)

type Rebuilder struct {
	snapshots *postgresql.SnapshotRepo
	stock     *redis.StockRepo
}

func NewRebuilder(rdb *goredis.Client, pool *pgxpool.Pool) *Rebuilder {
	return &Rebuilder{snapshots: postgresql.NewSnapshotRepo(pool), stock: redis.NewStockRepo(rdb)}
}

func (r *Rebuilder) Rebuild(ctx context.Context, activityID int64) error {
	snap, err := r.snapshots.LoadSnapshot(ctx, activityID)
	if err != nil {
		return fmt.Errorf("rebuild activity %d: %w", activityID, err)
	}
	if err := r.stock.Restore(ctx, snap); err != nil {
		return fmt.Errorf("rebuild activity %d: %w", activityID, err)
	}
	return nil
}
