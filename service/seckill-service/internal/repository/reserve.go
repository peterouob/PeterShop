package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/db"
)

var (
	ErrUnexpectedRows   = errors.New("unexpected affected rows")
	ErrDuplicateOrderNo = errors.New("duplicate order_no")
)

var txOpts = pgx.TxOptions{
	IsoLevel:   pgx.ReadCommitted,
	AccessMode: pgx.ReadWrite,
}

const NoDataFound = "P0002"

func (r *seckillRepoImpl) Reserve(ctx context.Context, orderNo, activityID int64, userID string) error {
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

var constraintErrs = map[string]error{
	"uk_ledger_activity_user": domain.ErrAlreadyBought,
	"ck_stock_non_negative":   domain.ErrSoldOut,
}

func classifyReserveErr(err error) error {
	if err == nil {
		return nil
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		if biz, ok := constraintErrs[pgErr.ConstraintName]; ok {
			return biz
		}
	}
	return fmt.Errorf("reserve: %w", err)
}
