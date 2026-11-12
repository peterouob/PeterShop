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

func TestLoadSnapshot_FreeSlotsAndEveryLedgerUser(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.slots(1, 6)
	f.activity(2, "p2", 100)
	f.slots(2, 6)
	require.NoError(t, f.repo.Reserve(f.ctx, 1, 1, "locked", 1))
	require.NoError(t, f.repo.Reserve(f.ctx, 2, 1, "sold", 2))
	f.exec(`UPDATE stock_ledger SET status = 2 WHERE order_no = 2`)
	require.NoError(t, f.repo.Reserve(f.ctx, 3, 1, "released", 3))
	f.release(3)
	require.NoError(t, f.repo.Reserve(f.ctx, 4, 1, "released-then-taken", 4))
	f.release(4)
	require.NoError(t, f.repo.Reserve(f.ctx, 5, 1, "taker", 4))
	f.disableSlot(1, 5)
	require.NoError(t, f.repo.Reserve(f.ctx, 9, 2, "other-activity", 6))

	snap, err := NewSnapshotRepo(f.pool).LoadSnapshot(f.ctx, 1)

	require.NoError(t, err)
	start, end := f.window(1)
	assert.EqualValues(t, 1, snap.ActivityID)
	assert.True(t, start.Equal(snap.StartAt), "start_at %v != %v", start, snap.StartAt)
	assert.True(t, end.Equal(snap.EndAt), "end_at %v != %v", end, snap.EndAt)
	assert.Equal(t, []int32{3, 6}, snap.Slots, "ordered; no disabled, no actively held slot; a slot whose only row is RELEASED is free")
	assert.ElementsMatch(t, []string{"locked", "sold", "released", "released-then-taken", "taker"}, snap.Users,
		"a RELEASED user still blocks a rebuy")
	assert.False(t, snap.Sealed)
}

func TestLoadSnapshot_NoWinners(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.slots(1, 10)

	snap, err := NewSnapshotRepo(f.pool).LoadSnapshot(f.ctx, 1)

	require.NoError(t, err)
	assert.Empty(t, snap.Users)
	assert.Equal(t, []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, snap.Slots)
}

func TestLoadSnapshot_NoFreeSlotsIsEmptyNotNull(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.slots(1, 2)
	require.NoError(t, f.repo.Reserve(f.ctx, 1, 1, "u1", 1))
	f.disableSlot(1, 2)

	snap, err := NewSnapshotRepo(f.pool).LoadSnapshot(f.ctx, 1)

	require.NoError(t, err)
	assert.NotNil(t, snap.Slots, "'{}' scans to an empty slice, NULL would scan to nil")
	assert.Empty(t, snap.Slots)
}

func TestLoadSnapshot_NoSlotRowsIsAnError(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)

	_, err := NewSnapshotRepo(f.pool).LoadSnapshot(f.ctx, 1)

	require.ErrorContains(t, err, "no slots row")
}

func TestLoadSnapshot_ClosedActivityIsSealed(t *testing.T) {
	f := newReserveFixture(t)
	f.activity(1, "p1", 100)
	f.slots(1, 10)
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
