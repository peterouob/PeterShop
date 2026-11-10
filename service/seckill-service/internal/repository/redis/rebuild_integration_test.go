//go:build integration

package redis

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rebuildAID int64 = 7

type rebuildFixture struct {
	t    *testing.T
	ctx  context.Context
	rdb  *redis.Client
	repo *StockRepo
	now  time.Time
}

func newRebuildFixture(t *testing.T) *rebuildFixture {
	t.Helper()
	ctx := context.Background()
	rdb := testkit.OpenRedis(t)
	now, err := rdb.Time(ctx).Result()
	require.NoError(t, err)
	return &rebuildFixture{t: t, ctx: ctx, rdb: rdb, repo: NewStockRepo(rdb), now: now}
}

func (f *rebuildFixture) snap(available int64, users ...string) domain.ActivitySnapshot {
	return domain.ActivitySnapshot{
		ActivityID: rebuildAID,
		StartAt:    f.now.Add(-time.Hour),
		EndAt:      f.now.Add(time.Hour),
		Available:  available,
		Users:      users,
	}
}

func (f *rebuildFixture) users() []string {
	f.t.Helper()
	m, err := f.rdb.SMembers(f.ctx, rediskey.Users(rebuildAID)).Result()
	require.NoError(f.t, err)
	return m
}

func (f *rebuildFixture) stock() int64 {
	f.t.Helper()
	n, err := f.rdb.Get(f.ctx, rediskey.Stock(rebuildAID)).Int64()
	require.NoError(f.t, err)
	return n
}

func (f *rebuildFixture) exists(key string) bool {
	f.t.Helper()
	n, err := f.rdb.Exists(f.ctx, key).Result()
	require.NoError(f.t, err)
	return n == 1
}

func TestRestore_EmptyRedis(t *testing.T) {
	f := newRebuildFixture(t)
	s := f.snap(8, "u1", "u2")

	require.NoError(t, f.repo.Restore(f.ctx, s))

	assert.ElementsMatch(t, []string{"u1", "u2"}, f.users())
	assert.EqualValues(t, 8, f.stock())
	meta, err := f.rdb.HGetAll(f.ctx, rediskey.Meta(rebuildAID)).Result()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"start_ms": strconv.FormatInt(s.StartAt.UnixMilli(), 10),
		"end_ms":   strconv.FormatInt(s.EndAt.UnixMilli(), 10),
	}, meta, "an open activity has no sealed field")

	assert.NoError(t, f.repo.DeductStock(f.ctx, rebuildAID, "u3"), "the rebuilt activity sells again")
	assert.ErrorIs(t, f.repo.DeductStock(f.ctx, rebuildAID, "u1"), domain.ErrAlreadyAdmitted)
}

func TestRestore_OverwritesStaleState(t *testing.T) {
	f := newRebuildFixture(t)
	require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.Users(rebuildAID), "u1", "stale").Err())
	require.NoError(t, f.rdb.Set(f.ctx, rediskey.Stock(rebuildAID), 99, 0).Err())

	require.NoError(t, f.repo.Restore(f.ctx, f.snap(5, "u1", "u2")))

	assert.ElementsMatch(t, []string{"u1", "u2"}, f.users())
	assert.EqualValues(t, 5, f.stock())
}

func TestRestore_NoUsersClearsSet(t *testing.T) {
	f := newRebuildFixture(t)
	require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.Users(rebuildAID), "stale").Err())

	require.NoError(t, f.repo.Restore(f.ctx, f.snap(10)))

	assert.False(t, f.exists(rediskey.Users(rebuildAID)))
	assert.EqualValues(t, 10, f.stock())
	assert.NoError(t, f.repo.DeductStock(f.ctx, rebuildAID, "stale"))
}

func TestRestore_IgnoresLeftoverScratch(t *testing.T) {
	f := newRebuildFixture(t)
	require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.UsersRebuild(rebuildAID), "junk").Err())

	require.NoError(t, f.repo.Restore(f.ctx, f.snap(9, "u1")))

	assert.ElementsMatch(t, []string{"u1"}, f.users())
	assert.True(t, f.exists(rediskey.UsersRebuild(rebuildAID)), "leftover scratch is never touched")
}

func TestRestore_ManyUsersAcrossBatches(t *testing.T) {
	f := newRebuildFixture(t)
	users := make([]string, 2*rebuildBatch+1)
	for i := range users {
		users[i] = fmt.Sprintf("u%d", i)
	}

	require.NoError(t, f.repo.Restore(f.ctx, f.snap(0, users...)))

	n, err := f.rdb.SCard(f.ctx, rediskey.Users(rebuildAID)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, len(users), n)
}

func TestRestore_SealedFromDB(t *testing.T) {
	f := newRebuildFixture(t)
	s := f.snap(10)
	s.Sealed = true

	require.NoError(t, f.repo.Restore(f.ctx, s))

	assert.ErrorIs(t, f.repo.DeductStock(f.ctx, rebuildAID, "u1"), domain.ErrSealed)
}

func TestRestore_NeverUnseals(t *testing.T) {
	f := newRebuildFixture(t)
	require.NoError(t, f.rdb.HSet(f.ctx, rediskey.Meta(rebuildAID), "sealed", 1).Err())

	require.NoError(t, f.repo.Restore(f.ctx, f.snap(10)))

	assert.ErrorIs(t, f.repo.DeductStock(f.ctx, rebuildAID, "u1"), domain.ErrSealed)
}

func TestSwapScript_MismatchTouchesNothing(t *testing.T) {
	f := newRebuildFixture(t)
	require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.Users(rebuildAID), "u1").Err())
	require.NoError(t, f.rdb.Set(f.ctx, rediskey.Stock(rebuildAID), 7, 0).Err())
	require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.UsersRebuild(rebuildAID), "x").Err())

	keys := []string{rediskey.Users(rebuildAID), rediskey.UsersRebuild(rebuildAID), rediskey.Stock(rebuildAID), rediskey.Meta(rebuildAID)}
	err := swapScript.Run(f.ctx, f.rdb, keys, 3, 1, 2, 2, "0").Err()

	require.ErrorContains(t, err, swapMismatchCode)
	assert.ElementsMatch(t, []string{"u1"}, f.users())
	assert.EqualValues(t, 7, f.stock())
	assert.False(t, f.exists(rediskey.Meta(rebuildAID)))
}
