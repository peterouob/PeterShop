//go:build integration

package postgresql

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const payTimeout = 15 * time.Minute

type pendingFixture struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	repo *PendingRepo
}

func newPendingFixture(t *testing.T) *pendingFixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.OpenContainer(t, "order")
	require.NoError(t, db.Migrator(t).Up())

	pool, err := pgxpool.New(ctx, db.DSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return &pendingFixture{t: t, ctx: ctx, pool: pool, repo: NewPendingRepo(pool)}
}

func reserved(orderNo int64, userID string) event.OrderReserved {
	return event.OrderReserved{
		OrderNo:     orderNo,
		UserID:      userID,
		ProductID:   "p1",
		ActivityID:  1,
		Amount:      100,
		CreatedAtMs: time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC).UnixMilli() + orderNo,
	}
}

func (f *pendingFixture) count() int {
	f.t.Helper()
	var n int
	require.NoError(f.t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM orders`).Scan(&n))
	return n
}

func TestInsertPending_Idempotent(t *testing.T) {
	f := newPendingFixture(t)
	a, b := reserved(1, "u1"), reserved(2, "u2")

	require.NoError(t, f.repo.InsertPendingOrders(f.ctx, []event.OrderReserved{a, a, b}, payTimeout))
	require.NoError(t, f.repo.InsertPendingOrders(f.ctx, []event.OrderReserved{a}, payTimeout))

	assert.Equal(t, 2, f.count())

	var status int16
	var createdAt, expireAt time.Time
	require.NoError(t, f.pool.QueryRow(f.ctx,
		`SELECT status, created_at, expire_at FROM orders WHERE order_no = $1`, a.OrderNo).
		Scan(&status, &createdAt, &expireAt))
	assert.EqualValues(t, 1, status)
	assert.Equal(t, a.CreatedAtMs, createdAt.UnixMilli(), "created_at is the reservation time, not now()")
	assert.Equal(t, payTimeout, expireAt.Sub(createdAt))
}

func TestInsertPending_SameUserActivity(t *testing.T) {
	f := newPendingFixture(t)

	require.NoError(t, f.repo.InsertPendingOrders(f.ctx, []event.OrderReserved{reserved(1, "u1"), reserved(2, "u1")}, payTimeout))

	assert.Equal(t, 1, f.count())
}
