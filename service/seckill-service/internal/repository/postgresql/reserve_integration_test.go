//go:build integration

package postgresql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/stretchr/testify/require"
)

type reserveFixture struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	repo *ReserveRepo
}

func newReserveFixture(t *testing.T) *reserveFixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.OpenContainer(t, "seckill")
	require.NoError(t, db.Migrator(t).Up())

	pool, err := pgxpool.New(ctx, db.DSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return &reserveFixture{t: t, ctx: ctx, pool: pool, repo: &ReserveRepo{pool: pool}}
}

func (f *reserveFixture) activity(aid int64, productID string, price int64) {
	f.t.Helper()
	start := time.Now().Add(-time.Minute)
	_, err := f.pool.Exec(f.ctx,
		`INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, 2, now(), now())`,
		aid, productID, price, start, start.Add(time.Hour))
	require.NoError(f.t, err)
}

func (f *reserveFixture) stock(aid int64, available, sold int) {
	f.t.Helper()
	_, err := f.pool.Exec(f.ctx,
		`INSERT INTO activity_stock (activity_id, total, available, locked, sold) VALUES ($1, $2, $3, 0, $4)`,
		aid, available+sold, available, sold)
	require.NoError(f.t, err)
}

func (f *reserveFixture) count(table string) int {
	f.t.Helper()
	var n int
	require.NoError(f.t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM `+table).Scan(&n))
	return n
}

func TestReserve_Success(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.stock(1, 10, 0)

	err := f.repo.Reserve(f.ctx, 1001, 1, "u1")

	require.NoError(t, err)
	require.Equal(t, 1, f.count("stock_ledger"))
	require.Zero(t, f.count("outbox"), "outbox must stay empty: Debezium reads the INSERT from WAL")
}

func TestReserve_AlreadyBought(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.stock(1, 10, 0)
	require.NoError(t, f.repo.Reserve(f.ctx, 1001, 1, "u1"))

	err := f.repo.Reserve(f.ctx, 1002, 1, "u1")
	require.ErrorIs(t, err, domain.ErrAlreadyBought)
}

func TestReserve_SoldOut(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.stock(1, 0, 10)

	err := f.repo.Reserve(f.ctx, 1001, 1, "u1")
	require.ErrorIs(t, err, domain.ErrSoldOut)
	require.Zero(t, f.count("stock_ledger"))
}

func TestReserve_NoActivity(t *testing.T) {
	f := newReserveFixture(t)

	err := f.repo.Reserve(f.ctx, 1001, 1, "u1")
	require.Error(t, err)

	_, isDomain := errors.AsType[*domain.Error](err)
	require.False(t, isDomain)
	require.ErrorIs(t, err, ErrUnexpectedRows)

	require.Equal(t, 0, f.count("stock_ledger"))
}
