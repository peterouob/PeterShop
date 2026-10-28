package idgenerate

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type movableWall struct {
	base   time.Time
	start  time.Time
	offset atomic.Int64
}

func newMovableWall(t time.Time) *movableWall {
	return &movableWall{base: t, start: time.Now()}
}

func (w *movableWall) now() time.Time {
	return w.base.Add(time.Since(w.start) + time.Duration(w.offset.Load()))
}
func (w *movableWall) shift(d time.Duration) { w.offset.Add(int64(d)) }

func startedSnowflake(t *testing.T, h *hookCoord, wall func() time.Time) *Snowflake {
	t.Helper()
	sf := &Snowflake{coord: h, clock: newClock(wall)}
	require.NoError(t, sf.start(context.Background()))
	return sf
}

func runLoop(t *testing.T, sf *Snowflake, interval time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	sf.cancel = cancel
	sf.done = make(chan struct{})
	sf.startLoop(ctx, interval)
	t.Cleanup(func() {
		cancel()
		<-sf.done
	})
}

func TestTick_NoDriftOnFreshSession(t *testing.T) {
	sf := startedSnowflake(t, &hookCoord{Coordinator: memory.NewWithClock(frozen)}, frozen)

	assert.NoError(t, sf.tick(context.Background()), "a fresh session must not report a clock jump")
}

func TestTick_ExtendsLease(t *testing.T) {
	sf := startedSnowflake(t, &hookCoord{Coordinator: memory.NewWithClock(frozen)}, frozen)
	s := sf.cur.Load()
	before := s.lease.deadLine.Load()
	time.Sleep(2 * time.Millisecond)

	require.NoError(t, sf.tick(context.Background()))
	assert.Greater(t, s.lease.deadLine.Load(), before)
}

func TestTick_RenewsReservation(t *testing.T) {
	sf := startedSnowflake(t, &hookCoord{Coordinator: memory.NewWithClock(frozen)}, frozen)
	s := sf.cur.Load()
	s.reserve.untilMs.Store(sf.clock.wallMs())

	require.NoError(t, sf.tick(context.Background()))
	assert.Greater(t, s.reserve.until(), sf.clock.wallMs())
}

func TestTick_KeepAliveFailWhileLeaseValid_KeepsSession(t *testing.T) {
	h := &hookCoord{Coordinator: memory.NewWithClock(frozen)}
	sf := startedSnowflake(t, h, frozen)
	s := sf.cur.Load()
	h.keepAlive = func(context.Context, int64) (time.Duration, error) { return 0, errors.New("etcd down") }

	assert.Error(t, sf.tick(context.Background()))
	assert.Same(t, s, sf.cur.Load(), "lease still valid locally: retry next tick, don't restart")
	assert.True(t, sf.Ready())
}

func TestTick_LeaseExpired_ReturnsErr(t *testing.T) {
	h := &hookCoord{Coordinator: memory.NewWithClock(frozen)}
	sf := startedSnowflake(t, h, frozen)
	old := sf.cur.Load()
	old.lease.deadLine.Store(0)

	assert.ErrorIs(t, sf.tick(context.Background()), ErrLeaseExpired)
	assert.Same(t, old, sf.cur.Load(), "tick itself must not restart")
}

func TestTick_KeepAliveHangs_ReturnsWithinTimeout(t *testing.T) {
	h := &hookCoord{Coordinator: memory.NewWithClock(frozen)}
	sf := startedSnowflake(t, h, frozen)
	h.keepAlive = func(ctx context.Context, _ int64) (time.Duration, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	}

	begin := time.Now()
	assert.Error(t, sf.tick(context.Background()))
	assert.Less(t, time.Since(begin), leaseTTL/6+500*time.Millisecond, "a hung KeepAlive must not eat the next tick")
}

func TestTick_SmallBackwardJump_Tolerated(t *testing.T) {
	wall := newMovableWall(frozenNow)
	sf := startedSnowflake(t, &hookCoord{Coordinator: memory.NewWithClock(wall.now)}, wall.now)
	wall.shift(-5 * time.Millisecond)

	assert.NoError(t, sf.tick(context.Background()))
	assert.True(t, sf.Ready())
}

func TestTick_LargeBackwardJump_ReturnsErr(t *testing.T) {
	wall := newMovableWall(frozenNow)
	sf := startedSnowflake(t, &hookCoord{Coordinator: memory.NewWithClock(wall.now)}, wall.now)
	wall.shift(-time.Second)

	assert.ErrorIs(t, sf.tick(context.Background()), ErrClockJump)
}

func TestLoop_LeaseExpired_Restarts(t *testing.T) {
	wall := newMovableWall(frozenNow)
	sf := startedSnowflake(t, &hookCoord{Coordinator: memory.NewWithClock(wall.now)}, wall.now)
	old := sf.cur.Load()
	old.lease.deadLine.Store(0)

	runLoop(t, sf, 5*time.Millisecond)

	require.Eventually(t, func() bool { return sf.cur.Load() != old }, time.Second, 5*time.Millisecond,
		"loop must restart on expired lease")
	assert.NotEqual(t, old.leaseID, sf.cur.Load().leaseID)
	assert.True(t, sf.Ready())
}

func TestLoop_ClockJump_StopsIssuing(t *testing.T) {
	wall := newMovableWall(frozenNow)
	sf := startedSnowflake(t, &hookCoord{Coordinator: memory.NewWithClock(wall.now)}, wall.now)

	runLoop(t, sf, 5*time.Millisecond)
	wall.shift(-time.Second)

	require.Eventually(t, func() bool { _, err := sf.Next(); return err != nil }, time.Second, 5*time.Millisecond,
		"must stop issuing after a large clock jump")
	time.Sleep(50 * time.Millisecond)
	assert.False(t, sf.Ready(), "loop must not bring the instance back after stopping it")
}

func TestLoop_CloseStopsLoop(t *testing.T) {
	var calls atomic.Int64
	wall := newMovableWall(frozenNow)
	h := &hookCoord{Coordinator: memory.NewWithClock(wall.now)}
	h.keepAlive = func(ctx context.Context, id int64) (time.Duration, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return h.Coordinator.KeepAlive(ctx, id)
	}
	sf := startedSnowflake(t, h, wall.now)

	runLoop(t, sf, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	require.Positive(t, calls.Load(), "loop should be ticking")

	require.NoError(t, sf.Close(context.Background()))
	after := calls.Load()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, after, calls.Load(), "no coord calls after Close returns")
	assert.False(t, sf.Ready(), "loop must not restart a closed instance")
}
