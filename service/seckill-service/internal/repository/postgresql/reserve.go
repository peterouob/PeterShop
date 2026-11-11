package postgresql

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
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

func (r *ReserveRepo) Reserve(ctx context.Context, orderNo, activityID int64, userID string, slotNo int32) error {
	return classifyReserveErr(pgx.BeginTxFunc(ctx, r.pool, txOpts, func(tx pgx.Tx) error {
		q := seckill.New(tx)

		n, err := q.InsertLedgerWithOutbox(ctx, seckill.InsertLedgerWithOutboxParams{
			OrderNo:    orderNo,
			ActivityID: activityID,
			UserID:     userID,
			SlotNo:     slotNo,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%w: order_no=%d", ErrDuplicateOrderNo, orderNo)
		}

		_, err = q.DeleteOutBox(ctx, orderNo)
		return err
	}))
}
