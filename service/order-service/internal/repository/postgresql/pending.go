package postgresql

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/event"
)

type PendingRepo struct {
	pool *pgxpool.Pool
}

func NewPendingRepo(pool *pgxpool.Pool) *PendingRepo {
	return &PendingRepo{pool: pool}
}

const insertPendingSQL = `
INSERT INTO orders(order_no, user_id, product_id, activity_id, amount, status, expire_at, created_at, updated_at) 
SELECT o, u, p, a, amt, 1, e, c, now()
FROM unnest(
     $1::bigint[], -- order_no
     $2::text[], -- user_id
     $3::text[], -- product_id
     $4::bigint[], -- activity_id
     $5::bigint[], -- amount
     $6::timestamptz[], -- created_at
     $7::timestamptz[] -- expire_at
) AS t(o, u, p, a, amt, c, e)
ON CONFLICT DO NOTHING
`

func (r *PendingRepo) InsertPendingOrders(ctx context.Context, evs []event.OrderReserved, payTimeout time.Duration) error {
	if len(evs) == 0 {
		return nil
	}

	n := len(evs)
	orderNos := make([]int64, n)
	userIDs := make([]string, n)
	productIDs := make([]string, n)
	activityIDs := make([]int64, n)
	amounts := make([]int64, n)
	createdAts := make([]time.Time, n)
	expireAts := make([]time.Time, n)

	for i, ev := range evs {
		orderNos[i] = ev.OrderNo
		userIDs[i] = ev.UserID
		productIDs[i] = ev.ProductID
		activityIDs[i] = ev.ActivityID
		amounts[i] = ev.Amount
		createdAts[i] = time.UnixMilli(ev.CreatedAtMs)
		expireAts[i] = time.UnixMilli(ev.ExpireAtMs(payTimeout))
	}

	if _, err := r.pool.Exec(ctx, insertPendingSQL, orderNos, userIDs, productIDs, activityIDs, amounts, createdAts, expireAts); err != nil {
		return fmt.Errorf("insert pending: %w", err)
	}
	return nil

}
