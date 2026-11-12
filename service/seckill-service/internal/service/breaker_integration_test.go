//go:build integration

package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBreaker_DBDownReturnsEverySlot(t *testing.T) {
	t.Parallel()
	const aid, slots, afterTrip = 1, 20, 10
	ctx := context.Background()

	rdb := testkit.OpenRedis(t)
	db := testkit.OpenContainer(t, "seckill")
	require.NoError(t, db.Migrator(t).Up())
	pool, err := pgxpool.New(ctx, db.DSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	redisNow, err := rdb.Time(ctx).Result()
	require.NoError(t, err)
	require.NoError(t, rdb.HSet(ctx, rediskey.Meta(aid), map[string]any{
		"start_ms": redisNow.Add(-time.Hour).UnixMilli(),
		"end_ms":   redisNow.Add(time.Hour).UnixMilli(),
	}).Err())
	members := make([]any, slots)
	for i := range members {
		members[i] = i + 1
	}
	require.NoError(t, rdb.SAdd(ctx, rediskey.Slots(aid), members...).Err())
	initial, err := rdb.SMembers(ctx, rediskey.Slots(aid)).Result()
	require.NoError(t, err)

	require.False(t, isSuccessful(errInjected), "the injected fault must count as a breaker failure")
	repo := &faultyRepo{
		SeckillRepo: repository.NewSeckillRepo(rdb, pool),
		plan:        func(string) fault { return faultCrash },
	}
	var seq int64
	svc := &seckillService{
		repo:           repo,
		nextID:         func() (int64, error) { seq++; return seq, nil },
		cb:             newReserveGate(time.Minute),
		reserveTimeout: reserveTimeout,
	}

	for _, uid := range userIDs(tripAfterFailures) {
		_, err := svc.Buy(ctx, aid, uid)
		require.ErrorIs(t, err, errInjected)
	}
	require.Equal(t, gobreaker.StateOpen, svc.cb.Breaker().State())

	for _, uid := range userIDsFrom(tripAfterFailures, afterTrip) {
		_, err := svc.Buy(ctx, aid, uid)
		assert.ErrorIs(t, err, domain.ErrUnavailable)
	}

	assert.EqualValues(t, tripAfterFailures, repo.crashes.Load(), "only pre-trip calls may reach the db")

	after, err := rdb.SMembers(ctx, rediskey.Slots(aid)).Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initial, after, "every deducted slot must be returned")

	users, err := rdb.SCard(ctx, rediskey.Users(aid)).Result()
	require.NoError(t, err)
	assert.Zero(t, users, "every admitted user must be removed")

	var active int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM stock_ledger WHERE activity_id = $1 AND status <> 3`, aid).Scan(&active))
	assert.Zero(t, active)
}
