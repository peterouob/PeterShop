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

func seq(n int) []int32 {
	out := make([]int32, n)
	for i := range out {
		out[i] = int32(i + 1)
	}
	return out
}

func (f *rebuildFixture) snap(slots []int32, users ...string) domain.ActivitySnapshot {
	return domain.ActivitySnapshot{
		ActivityID: rebuildAID,
		StartAt:    f.now.Add(-time.Hour),
		EndAt:      f.now.Add(time.Hour),
		Slots:      slots,
		Users:      users,
	}
}

func (f *rebuildFixture) users() []string {
	f.t.Helper()
	m, err := f.rdb.SMembers(f.ctx, rediskey.Users(rebuildAID)).Result()
	require.NoError(f.t, err)
	return m
}

func (f *rebuildFixture) slots() []int32 {
	f.t.Helper()
	m, err := f.rdb.SMembers(f.ctx, rediskey.Slots(rebuildAID)).Result()
	require.NoError(f.t, err)
	out := make([]int32, len(m))
	for i, s := range m {
		n, err := strconv.ParseInt(s, 10, 32)
		require.NoError(f.t, err)
		out[i] = int32(n)
	}
	return out
}

func (f *rebuildFixture) meta() map[string]string {
	f.t.Helper()
	m, err := f.rdb.HGetAll(f.ctx, rediskey.Meta(rebuildAID)).Result()
	require.NoError(f.t, err)
	return m
}

func (f *rebuildFixture) exists(key string) bool {
	f.t.Helper()
	n, err := f.rdb.Exists(f.ctx, key).Result()
	require.NoError(f.t, err)
	return n == 1
}

func TestRestore_EmptyRedis(t *testing.T) {
	f := newRebuildFixture(t)
	s := f.snap(seq(8), "u1", "u2")

	require.NoError(t, f.repo.Restore(f.ctx, s))

	assert.ElementsMatch(t, []string{"u1", "u2"}, f.users())
	assert.ElementsMatch(t, seq(8), f.slots())
	assert.Equal(t, map[string]string{
		"start_ms": strconv.FormatInt(s.StartAt.UnixMilli(), 10),
		"end_ms":   strconv.FormatInt(s.EndAt.UnixMilli(), 10),
	}, f.meta(), "an open activity with free slots has neither sealed nor drained")

	_, err := f.repo.DeductStock(f.ctx, rebuildAID, "u3")
	assert.NoError(t, err, "the rebuilt activity sells again")
	assert.ErrorIs(t, deductErr(f.repo.DeductStock(f.ctx, rebuildAID, "u1")), domain.ErrAlreadyAdmitted)
}

func TestRestore_OverwritesStaleState(t *testing.T) {
	f := newRebuildFixture(t)
	require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.Users(rebuildAID), "u1", "stale").Err())
	require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.Slots(rebuildAID), 1, 99).Err())

	require.NoError(t, f.repo.Restore(f.ctx, f.snap([]int32{2, 3}, "u1", "u2")))

	assert.ElementsMatch(t, []string{"u1", "u2"}, f.users())
	assert.ElementsMatch(t, []int32{2, 3}, f.slots())
}

func TestRestore_NoUsersClearsSet(t *testing.T) {
	f := newRebuildFixture(t)
	require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.Users(rebuildAID), "stale").Err())

	require.NoError(t, f.repo.Restore(f.ctx, f.snap(seq(10))))

	assert.False(t, f.exists(rediskey.Users(rebuildAID)))
	assert.ElementsMatch(t, seq(10), f.slots())
	_, err := f.repo.DeductStock(f.ctx, rebuildAID, "stale")
	assert.NoError(t, err)
}

func TestRestore_IgnoresLeftoverScratch(t *testing.T) {
	f := newRebuildFixture(t)
	require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.UsersRebuild(rebuildAID), "junk").Err())
	require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.SlotsRebuild(rebuildAID), 42).Err())

	require.NoError(t, f.repo.Restore(f.ctx, f.snap(seq(9), "u1")))

	assert.ElementsMatch(t, []string{"u1"}, f.users())
	assert.ElementsMatch(t, seq(9), f.slots())
	assert.True(t, f.exists(rediskey.UsersRebuild(rebuildAID)), "leftover scratch is never touched")
	assert.True(t, f.exists(rediskey.SlotsRebuild(rebuildAID)), "leftover scratch is never touched")
}

func TestRestore_ManyMembersAcrossBatches(t *testing.T) {
	f := newRebuildFixture(t)
	users := make([]string, 2*rebuildBatch+1)
	for i := range users {
		users[i] = fmt.Sprintf("u%d", i)
	}

	require.NoError(t, f.repo.Restore(f.ctx, f.snap(seq(3*rebuildBatch+7), users...)))

	n, err := f.rdb.SCard(f.ctx, rediskey.Users(rebuildAID)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, len(users), n)
	n, err = f.rdb.SCard(f.ctx, rediskey.Slots(rebuildAID)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, 3*rebuildBatch+7, n)
	ttl, err := f.rdb.PTTL(f.ctx, rediskey.Slots(rebuildAID)).Result()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(-1), ttl, "the live slots set never inherits the scratch expiry")
	ttl, err = f.rdb.PTTL(f.ctx, rediskey.Users(rebuildAID)).Result()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(-1), ttl, "the live users set never inherits the scratch expiry")
}

func TestRestore_RejectsNonPositiveSlot(t *testing.T) {
	f := newRebuildFixture(t)

	require.Error(t, f.repo.Restore(f.ctx, f.snap([]int32{1, 0})))

	assert.False(t, f.exists(rediskey.Meta(rebuildAID)), "an invalid snapshot never reaches Redis")
}

func TestRestore_SealedFromDB(t *testing.T) {
	f := newRebuildFixture(t)
	s := f.snap(seq(10))
	s.Sealed = true

	require.NoError(t, f.repo.Restore(f.ctx, s))

	assert.ErrorIs(t, deductErr(f.repo.DeductStock(f.ctx, rebuildAID, "u1")), domain.ErrSealed)
}

func TestRestore_NeverUnseals(t *testing.T) {
	f := newRebuildFixture(t)
	require.NoError(t, f.rdb.HSet(f.ctx, rediskey.Meta(rebuildAID), "sealed", 1).Err())

	require.NoError(t, f.repo.Restore(f.ctx, f.snap(seq(10))))

	assert.ErrorIs(t, deductErr(f.repo.DeductStock(f.ctx, rebuildAID, "u1")), domain.ErrSealed)
}

type liveState struct {
	users []string
	slots []int32
	meta  map[string]string
}

func (f *rebuildFixture) live() liveState {
	return liveState{users: f.users(), slots: f.slots(), meta: f.meta()}
}

func TestSwapScript_MismatchTouchesNothing(t *testing.T) {
	cases := []struct {
		name               string
		expUsers, expSlots int
	}{
		{name: "wrong expected slots", expUsers: 1, expSlots: 3},
		{name: "wrong expected users", expUsers: 2, expSlots: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRebuildFixture(t)
			usersTmp, slotsTmp := rediskey.UsersRebuild(rebuildAID), rediskey.SlotsRebuild(rebuildAID)
			require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.Users(rebuildAID), "u1").Err())
			require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.Slots(rebuildAID), 7).Err())
			require.NoError(t, f.rdb.HSet(f.ctx, rediskey.Meta(rebuildAID), "start_ms", 1, "end_ms", 2).Err())
			require.NoError(t, f.rdb.SAdd(f.ctx, usersTmp, "x").Err())
			require.NoError(t, f.rdb.SAdd(f.ctx, slotsTmp, 1, 2).Err())
			before := f.live()

			keys := []string{rediskey.Users(rebuildAID), usersTmp, rediskey.Slots(rebuildAID), slotsTmp, rediskey.Meta(rebuildAID)}
			err := swapScript.Run(f.ctx, f.rdb, keys, 10, 20, tc.expUsers, tc.expSlots, "0").Err()

			require.ErrorContains(t, err, swapMismatchCode)
			assert.Equal(t, before, f.live())
			assert.True(t, f.exists(usersTmp), "scratch is left for its TTL")
			assert.True(t, f.exists(slotsTmp), "scratch is left for its TTL")
		})
	}
}

func TestSwapScript_BadArgTouchesNothing(t *testing.T) {
	cases := []struct {
		name               string
		expUsers, expSlots any
		sealed             string
	}{
		{name: "negative slots", expUsers: 1, expSlots: -1, sealed: "0"},
		{name: "fractional slots", expUsers: 1, expSlots: "1.5", sealed: "0"},
		{name: "non-numeric slots", expUsers: 1, expSlots: "abc", sealed: "0"},
		{name: "negative users", expUsers: -1, expSlots: 2, sealed: "0"},
		{name: "non-numeric users", expUsers: "x", expSlots: 2, sealed: "0"},
		{name: "bad sealed flag", expUsers: 1, expSlots: 2, sealed: "2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRebuildFixture(t)
			usersTmp, slotsTmp := rediskey.UsersRebuild(rebuildAID), rediskey.SlotsRebuild(rebuildAID)
			require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.Users(rebuildAID), "u1").Err())
			require.NoError(t, f.rdb.SAdd(f.ctx, rediskey.Slots(rebuildAID), 7).Err())
			require.NoError(t, f.rdb.HSet(f.ctx, rediskey.Meta(rebuildAID), "start_ms", 1, "end_ms", 2, "drained", 1).Err())
			require.NoError(t, f.rdb.SAdd(f.ctx, usersTmp, "x").Err())
			require.NoError(t, f.rdb.SAdd(f.ctx, slotsTmp, 1, 2).Err())
			before := f.live()

			keys := []string{rediskey.Users(rebuildAID), usersTmp, rediskey.Slots(rebuildAID), slotsTmp, rediskey.Meta(rebuildAID)}
			err := swapScript.Run(f.ctx, f.rdb, keys, 10, 20, tc.expUsers, tc.expSlots, tc.sealed).Err()

			require.ErrorContains(t, err, "REBUILD_BAD_ARG")
			assert.Equal(t, before, f.live())
		})
	}
}
