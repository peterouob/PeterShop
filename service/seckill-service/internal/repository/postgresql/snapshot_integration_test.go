//go:build integration

package postgresql

import (
	"testing"
	"time"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (f *reserveFixture) window(aid int64) (start, end time.Time) {
	f.t.Helper()
	require.NoError(f.t, f.pool.QueryRow(f.ctx,
		`SELECT start_at, end_at FROM seckill_activity WHERE activity_id = $1`, aid).Scan(&start, &end))
	return start, end
}

func (f *reserveFixture) exec(sql string, args ...any) {
	f.t.Helper()
	_, err := f.pool.Exec(f.ctx, sql, args...)
	require.NoError(f.t, err)
}

func TestLoadSnapshot_ActivityStockAndEveryLedgerUser(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.stock(1, 10, 0)
	f.activity(2, "p2", 100)
	f.stock(2, 10, 0)
	for i, uid := range []string{"u1", "u2", "u3"} {
		require.NoError(t, f.repo.Reserve(f.ctx, int64(i+1), 1, uid))
	}
	require.NoError(t, f.repo.Reserve(f.ctx, 9, 2, "other-activity"))
	// u2 timed out and was released: still in the ledger, so uk_ledger_activity_user still blocks a rebuy.
	f.exec(`UPDATE stock_ledger SET status = 3 WHERE order_no = 2`)
	f.exec(`UPDATE activity_stock SET available = available + 1, locked = locked - 1 WHERE activity_id = 1`)

	snap, err := NewSnapshotRepo(f.pool).LoadSnapshot(f.ctx, 1)

	require.NoError(t, err)
	start, end := f.window(1)
	assert.EqualValues(t, 1, snap.ActivityID)
	assert.True(t, start.Equal(snap.StartAt), "start_at %v != %v", start, snap.StartAt)
	assert.True(t, end.Equal(snap.EndAt), "end_at %v != %v", end, snap.EndAt)
	assert.EqualValues(t, 8, snap.Available)
	assert.ElementsMatch(t, []string{"u1", "u2", "u3"}, snap.Users)
	assert.False(t, snap.Sealed)
}

func TestLoadSnapshot_NoWinners(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.stock(1, 10, 0)

	snap, err := NewSnapshotRepo(f.pool).LoadSnapshot(f.ctx, 1)

	require.NoError(t, err)
	assert.Empty(t, snap.Users)
	assert.EqualValues(t, 10, snap.Available)
}

func TestLoadSnapshot_ClosedActivityIsSealed(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.stock(1, 10, 0)
	f.exec(`UPDATE seckill_activity SET status = 3 WHERE activity_id = 1`)

	snap, err := NewSnapshotRepo(f.pool).LoadSnapshot(f.ctx, 1)

	require.NoError(t, err)
	assert.True(t, snap.Sealed)
}

func TestLoadSnapshot_UnknownActivity(t *testing.T) {
	f := newReserveFixture(t)

	_, err := NewSnapshotRepo(f.pool).LoadSnapshot(f.ctx, 404)

	require.ErrorIs(t, err, domain.ErrActivityNotFound)
}
