package postgresql

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/postgresql/db"
)

var txOpts = pgx.TxOptions{
	IsoLevel:   pgx.ReadCommitted,
	AccessMode: pgx.ReadWrite,
}

type ReserveRepo struct {
	pool *pgxpool.Pool
}

func NewReserveRepo(pool *pgxpool.Pool) *ReserveRepo {
	return &ReserveRepo{pool: pool}
}

func (r *ReserveRepo) Reserve(ctx context.Context, orderNo, activityID int64, userID string) error {
	insertOutBoxParams := seckill.InsertOutBoxParams{
		OrderNo:    orderNo,
		ActivityID: activityID,
		UserID:     userID,
	}

	stockLockParams := seckill.StockLockParams{
		OrderNo:    orderNo,
		ActivityID: activityID,
		UserID:     userID,
	}

	return classifyReserveErr(pgx.BeginTxFunc(ctx, r.pool, txOpts, func(tx pgx.Tx) error {
		quires := seckill.New(tx)

		if err := insertOutBox(ctx, quires, insertOutBoxParams); err != nil {
			return err
		}

		if _, err := quires.DeleteOutBox(ctx, insertOutBoxParams.OrderNo); err != nil {
			return err
		}

		locked, err := quires.StockLock(ctx, stockLockParams)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == NoDataFound {
				return fmt.Errorf("activity_stock missing: activity_id=%d: %w", activityID, err)
			}
			return fmt.Errorf("stock lock: %w", err)
		}

		if !locked {
			return fmt.Errorf("%w: order_no=%d", ErrDuplicateOrderNo, orderNo)
		}
		return nil
	}))
}

func insertOutBox(ctx context.Context, q *seckill.Queries, p seckill.InsertOutBoxParams) error {
	n, err := q.InsertOutBox(ctx, p)
	if err != nil {
		return fmt.Errorf("insert outbox: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("insert outbox order_no=%v activity_id=%v rows=%d: %w",
			p.OrderNo, p.ActivityID, n, ErrUnexpectedRows)
	}
	return nil
}
