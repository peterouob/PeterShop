package idgenerate

import (
	"context"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var frozenNow = time.UnixMilli(epochMs + 1_000_000)

func frozen() time.Time { return frozenNow }

func newLifecycleSnowflake(c coord.Coordinator) *Snowflake {
	sf := NewSnowflake(c)
	sf.clock = newClock(frozen)
	return sf
}

type hookCoord struct {
	coord.Coordinator
	grant    func(ctx context.Context, ttl time.Duration) (*coord.LeaseGrantResp, error)
	claim    func(ctx context.Context, workerID uint64, leaseID int64, owner string) (int64, bool, error)
	occupied func(ctx context.Context) ([]uint64, error)
}

func (h *hookCoord) Grant(ctx context.Context, ttl time.Duration) (*coord.LeaseGrantResp, error) {
	if h.grant != nil {
		return h.grant(ctx, ttl)
	}
	return h.Coordinator.Grant(ctx, ttl)
}

func (h *hookCoord) Claim(ctx context.Context, workerID uint64, leaseID int64, owner string) (int64, bool, error) {
	if h.claim != nil {
		return h.claim(ctx, workerID, leaseID, owner)
	}
	return h.Coordinator.Claim(ctx, workerID, leaseID, owner)
}

func (h *hookCoord) Occupied(ctx context.Context) ([]uint64, error) {
	if h.occupied != nil {
		return h.occupied(ctx)
	}
	return h.Coordinator.Occupied(ctx)
}

func TestStart_SingleInstance(t *testing.T) {
	c := memory.NewWithClock(frozen)
	sf := newLifecycleSnowflake(c)

	require.NoError(t, sf.start(context.Background()))

	id, err := sf.Next()
	require.NoError(t, err)
	ts, workerID, _ := decompose(id)
	assert.Equal(t, sf.clock.wallMs(), ts)
	assert.Equal(t, uint64(0), workerID)
	assert.Equal(t, sf.clock.wallMs()+reserveAhead.Milliseconds(), sf.cur.Load().reserve.until())
}

func TestStart_TwoInstancesGetDistinctWorkers(t *testing.T) {
	c := memory.NewWithClock(frozen)
	a, b := newLifecycleSnowflake(c), newLifecycleSnowflake(c)

	require.NoError(t, a.start(context.Background()))
	require.NoError(t, b.start(context.Background()))

	assert.NotEqual(t, a.cur.Load().gen.workerID, b.cur.Load().gen.workerID)
}

func TestClaim_StaleOccupiedSkipsTakenID(t *testing.T) {
	c := memory.NewWithClock(frozen)
	other, err := c.Grant(context.Background(), leaseTTL)
	require.NoError(t, err)
	_, ok, err := c.Claim(context.Background(), 0, other.ID, "other")
	require.True(t, ok)
	require.NoError(t, err)

	stale := &hookCoord{Coordinator: c, occupied: func(context.Context) ([]uint64, error) { return nil, nil }}
	sf := newLifecycleSnowflake(stale)

	require.NoError(t, sf.start(context.Background()))
	assert.Equal(t, uint64(1), sf.cur.Load().gen.workerID)
}

func TestRestart_SameMillisecondNoDuplicate(t *testing.T) {
	ctx := context.Background()
	c := memory.NewWithClock(frozen)

	first := newLifecycleSnowflake(c)
	require.NoError(t, first.start(ctx))
	issued := map[int64]bool{}
	for range 5 {
		id, err := first.Next()
		require.NoError(t, err)
		issued[id] = true
	}
	s := first.cur.Load()
	lastMs, ok := s.close()
	require.True(t, ok)
	require.NoError(t, c.Release(ctx, s.gen.workerID, s.leaseID, lastMs))
	require.NoError(t, c.Revoke(ctx, s.leaseID))

	second := newLifecycleSnowflake(c)
	require.NoError(t, second.start(ctx))
	require.Equal(t, s.gen.workerID, second.cur.Load().gen.workerID, "precondition: reclaim the same worker")

	id, err := second.Next()
	require.NoError(t, err)
	assert.False(t, issued[id], "duplicate ID %d issued after restart", id)
}

func TestStart_ClaimTimeoutFreesWorker(t *testing.T) {
	c := memory.NewWithClock(frozen)
	wallMs := newClock(frozen).wallMs()

	prev, err := c.Grant(context.Background(), leaseTTL)
	require.NoError(t, err)
	_, _, err = c.Claim(context.Background(), 0, prev.ID, "prev")
	require.NoError(t, err)
	require.NoError(t, c.Reserve(context.Background(), 0, prev.ID, wallMs+15))
	require.NoError(t, c.Release(context.Background(), 0, prev.ID, 0))

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	sf := newLifecycleSnowflake(c)
	require.ErrorIs(t, sf.start(ctx), context.DeadlineExceeded)

	probe, err := c.Grant(context.Background(), leaseTTL)
	require.NoError(t, err)
	_, ok, err := c.Claim(context.Background(), 0, probe.ID, "probe")
	assert.NoError(t, err, "worker 0 should be released after start fails")
	assert.True(t, ok)
}

func TestStart_LeaseDeadlineCountsFromGrant(t *testing.T) {
	c := memory.NewWithClock(frozen)
	sf := newLifecycleSnowflake(nil)

	var grantSentAt time.Duration
	sf.coord = &hookCoord{
		Coordinator: c,
		grant: func(ctx context.Context, ttl time.Duration) (*coord.LeaseGrantResp, error) {
			grantSentAt = sf.clock.mono()
			return c.Grant(ctx, ttl)
		},
		claim: func(ctx context.Context, workerID uint64, leaseID int64, owner string) (int64, bool, error) {
			time.Sleep(100 * time.Millisecond)
			return c.Claim(ctx, workerID, leaseID, owner)
		},
	}

	require.NoError(t, sf.start(context.Background()))

	limit := grantSentAt + leaseTTL - safetyMargin
	got := time.Duration(sf.cur.Load().lease.deadLine.Load())
	assert.LessOrEqual(t, got, limit, "local lease deadline exceeds server expiry minus margin by %v", got-limit)
}

func TestWait(t *testing.T) {
	sf := newLifecycleSnowflake(nil)
	now := sf.clock.wallMs()
	ctx := context.Background()

	cases := []struct {
		name   string
		prevMs int64
		skip   bool
	}{
		{"no reservation", coord.NoReservation, false},
		{"past", now - 1, false},
		{"now", now, false},
		{"short wait", now + 5, false},
		{"exactly max", now + maxWaitTime.Milliseconds(), false},
		{"too far", now + maxWaitTime.Milliseconds() + 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			skip, err := sf.wait(ctx, tc.prevMs)
			assert.NoError(t, err)
			assert.Equal(t, tc.skip, skip)
		})
	}
}
