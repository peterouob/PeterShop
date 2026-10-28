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

func TestNew_OK(t *testing.T) {
	sf, err := New(context.Background(), memory.New())
	require.NoError(t, err)
	require.NotNil(t, sf)

	assert.True(t, sf.Ready())
	_, err = sf.Next()
	assert.NoError(t, err)
}

func TestNew_ExhaustedGivesUpAtDeadline(t *testing.T) {
	c := memory.New()
	ctx := context.Background()
	other, err := c.Grant(ctx, time.Minute)
	require.NoError(t, err)
	for id := range uint64(maxWorkerID + 1) {
		_, ok, err := c.Claim(ctx, id, other.ID, "other")
		require.NoError(t, err)
		require.True(t, ok)
	}

	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	sf, err := New(ctx, c)

	assert.Error(t, err)
	assert.Nil(t, sf)
	assert.Less(t, time.Since(start), time.Second, "New must respect ctx deadline")
}

func TestNew_RetriesUntilWorkerFreed(t *testing.T) {
	c := memory.New()
	ctx := context.Background()
	other, err := c.Grant(ctx, time.Minute)
	require.NoError(t, err)
	for id := range uint64(maxWorkerID + 1) {
		_, _, err := c.Claim(ctx, id, other.ID, "other")
		require.NoError(t, err)
	}

	time.AfterFunc(300*time.Millisecond, func() {
		_ = c.Release(ctx, 42, other.ID, 0)
	})

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	sf, err := New(ctx, c)
	require.NoError(t, err)

	id, err := sf.Next()
	require.NoError(t, err)
	_, workerID, _ := decompose(id)
	assert.Equal(t, uint64(42), workerID)
}

func TestReady_Lifecycle(t *testing.T) {
	sf := newLifecycleSnowflake(memory.NewWithClock(frozen))
	assert.False(t, sf.Ready(), "before start")

	require.NoError(t, sf.start(context.Background()))
	assert.True(t, sf.Ready(), "after start")

	require.NoError(t, sf.Close(context.Background()))
	assert.False(t, sf.Ready(), "after Close")
}

func TestClose_NextReturnsLeaseExpired(t *testing.T) {
	sf := newLifecycleSnowflake(memory.NewWithClock(frozen))
	require.NoError(t, sf.start(context.Background()))
	require.NoError(t, sf.Close(context.Background()))

	_, err := sf.Next()
	assert.ErrorIs(t, err, ErrLeaseExpired)
}

func TestClose_ReleasesWorkerWithoutDuplicate(t *testing.T) {
	ctx := context.Background()
	c := memory.NewWithClock(frozen)

	a := newLifecycleSnowflake(c)
	require.NoError(t, a.start(ctx))
	var last int64
	for range 100 {
		id, err := a.Next()
		require.NoError(t, err)
		last = id
	}
	_, workerA, _ := decompose(last)
	require.NoError(t, a.Close(ctx))

	occupied, err := c.Occupied(ctx)
	require.NoError(t, err)
	assert.NotContains(t, occupied, workerA)

	b := newLifecycleSnowflake(c)
	require.NoError(t, b.start(ctx))
	id, err := b.Next()
	require.NoError(t, err)
	_, workerB, _ := decompose(id)
	assert.Equal(t, workerA, workerB)
	assert.Greater(t, id, last)
}

func TestClose_Idempotent(t *testing.T) {
	ctx := context.Background()
	revokes := 0
	c := &countRevoke{Coordinator: memory.NewWithClock(frozen), n: &revokes}
	sf := newLifecycleSnowflake(c)
	require.NoError(t, sf.start(ctx))

	require.NoError(t, sf.Close(ctx))
	require.NoError(t, sf.Close(ctx))
	assert.Equal(t, 1, revokes)
}

type countRevoke struct {
	coord.Coordinator
	n *int
}

func (c *countRevoke) Revoke(ctx context.Context, leaseID int64) error {
	*c.n++
	return c.Coordinator.Revoke(ctx, leaseID)
}
