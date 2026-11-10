//go:build integration

package redis

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleTimeout_Score(t *testing.T) {
	ctx := context.Background()
	rdb := testkit.OpenRedis(t)
	repo := NewTimeoutRepo(rdb)
	const payTimeout = 15 * time.Minute
	a := event.OrderReserved{OrderNo: 1, UserID: "u1", ProductID: "p1", ActivityID: 1, Amount: 100,
		CreatedAtMs: time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC).UnixMilli()}

	require.NoError(t, repo.ScheduleExpiry(ctx, []event.OrderReserved{a, a}, payTimeout))
	require.NoError(t, repo.ScheduleExpiry(ctx, []event.OrderReserved{a}, payTimeout))

	zs, err := rdb.ZRangeWithScores(ctx, rediskey.OrderExpiryQueue, 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, zs, 1)
	assert.Equal(t, strconv.FormatInt(a.OrderNo, 10), zs[0].Member)
	assert.Equal(t, float64(a.CreatedAtMs+payTimeout.Milliseconds()), zs[0].Score)
}
