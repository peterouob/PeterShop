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
	"github.com/stretchr/testify/assert"
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

func (f *reserveFixture) exec(sql string, args ...any) {
	f.t.Helper()
	_, err := f.pool.Exec(f.ctx, sql, args...)
	require.NoError(f.t, err)
}

func (f *reserveFixture) activity(aid int64, productID string, price int64) {
	f.t.Helper()
	start := time.Now().Add(-time.Minute)
	f.exec(`INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, 2, now(), now())`,
		aid, productID, price, start, start.Add(time.Hour))
}

func (f *reserveFixture) slots(aid int64, n int) {
	f.t.Helper()
	f.exec(`INSERT INTO activity_slot (activity_id, slot_no) SELECT $1, g FROM generate_series(1, $2::int) g`, aid, n)
}

func (f *reserveFixture) disableSlot(aid int64, slotNo int32) {
	f.t.Helper()
	f.exec(`UPDATE activity_slot SET disabled_at = now() WHERE activity_id = $1 AND slot_no = $2`, aid, slotNo)
}

func (f *reserveFixture) release(orderNo int64) {
	f.t.Helper()
	f.exec(`UPDATE stock_ledger SET status = 3 WHERE order_no = $1`, orderNo)
}

func (f *reserveFixture) count(table string) int {
	f.t.Helper()
	var n int
	require.NoError(f.t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM `+table).Scan(&n))
	return n
}

type reserveRows struct{ ledger, outbox int }

func (f *reserveFixture) rows() reserveRows {
	f.t.Helper()
	return reserveRows{ledger: f.count("stock_ledger"), outbox: f.count("outbox")}
}

func requireBusiness(t *testing.T, err, want error) {
	t.Helper()
	require.ErrorIs(t, err, want)
	_, isBiz := errors.AsType[*domain.Error](err)
	require.True(t, isBiz, "a rejection by a constraint is a business result")
}

func TestReserve_Success(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.slots(1, 3)

	require.NoError(t, f.repo.Reserve(f.ctx, 1001, 1, "u1", 2))

	var (
		slotNo             int32
		status             int16
		createdAt, updated *time.Time
	)
	require.NoError(t, f.pool.QueryRow(f.ctx,
		`SELECT slot_no, status, created_at, updated_at FROM stock_ledger WHERE order_no = 1001`).
		Scan(&slotNo, &status, &createdAt, &updated))
	assert.EqualValues(t, 2, slotNo)
	assert.EqualValues(t, 1, status, "a fresh reservation is Locked")
	assert.NotNil(t, createdAt)
	assert.NotNil(t, updated)
	assert.Zero(t, f.count("outbox"), "outbox must stay empty: Debezium reads the INSERT from WAL")
}

// A retried RPC reuses its order_no; the retry must not look like a user-facing rejection.
func TestReserve_DuplicateOrderNo(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.slots(1, 3)
	require.NoError(t, f.repo.Reserve(f.ctx, 1001, 1, "u1", 1))

	err := f.repo.Reserve(f.ctx, 1001, 1, "u1", 1)

	require.ErrorIs(t, err, ErrDuplicateOrderNo)
	_, isBiz := errors.AsType[*domain.Error](err)
	require.False(t, isBiz, "a duplicate order_no is a system error")
	require.Equal(t, reserveRows{ledger: 1, outbox: 0}, f.rows())
}

func TestReserve_Rejected(t *testing.T) {
	cases := []struct {
		name   string
		seed   func(f *reserveFixture)
		userID string
		slotNo int32
		want   error
	}{
		{
			name:   "already bought on another slot",
			seed:   func(f *reserveFixture) { require.NoError(f.t, f.repo.Reserve(f.ctx, 1, 1, "u1", 1)) },
			userID: "u1", slotNo: 2, want: domain.ErrAlreadyBought,
		},
		{
			name:   "slot taken",
			seed:   func(f *reserveFixture) { require.NoError(f.t, f.repo.Reserve(f.ctx, 1, 1, "u1", 1)) },
			userID: "u2", slotNo: 1, want: domain.ErrSlotTaken,
		},
		{
			name:   "slot not found",
			userID: "u1", slotNo: 99, want: domain.ErrSlotNotFound,
		},
		{
			name:   "slot disabled",
			seed:   func(f *reserveFixture) { f.disableSlot(1, 2) },
			userID: "u1", slotNo: 2, want: domain.ErrSlotDisabled,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReserveFixture(t)
			f.activity(1, "p1", 100)
			f.slots(1, 3)
			if tc.seed != nil {
				tc.seed(f)
			}
			before := f.rows()

			err := f.repo.Reserve(f.ctx, 1001, 1, tc.userID, tc.slotNo)

			requireBusiness(t, err, tc.want)
			require.Equal(t, before, f.rows(), "a rejected reserve must roll back ledger and outbox")
		})
	}
}

// Slots are referenced by activity_slot, which is referenced by seckill_activity, so an unknown activity has no slot.
func TestReserve_NoActivity(t *testing.T) {
	f := newReserveFixture(t)

	err := f.repo.Reserve(f.ctx, 1001, 404, "u1", 1)

	requireBusiness(t, err, domain.ErrSlotNotFound)
	require.Equal(t, reserveRows{}, f.rows())
}

func TestReserve_ReleasedSlotIsReusable(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.slots(1, 1)
	require.NoError(t, f.repo.Reserve(f.ctx, 1001, 1, "u1", 1))
	f.release(1001)

	require.NoError(t, f.repo.Reserve(f.ctx, 1002, 1, "u2", 1))

	var active int
	require.NoError(t, f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM stock_ledger WHERE activity_id = 1 AND slot_no = 1 AND status <> 3`).Scan(&active))
	require.Equal(t, 1, active)
}
