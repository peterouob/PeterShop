//go:build integration

package redis

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const margin = time.Minute

type window struct{ start, end int64 }

func open(now int64) window { return window{now - margin.Milliseconds(), now + margin.Milliseconds()} }
func notYet(now int64) window {
	return window{now + margin.Milliseconds(), now + 2*margin.Milliseconds()}
}
func ended(now int64) window {
	return window{now - 2*margin.Milliseconds(), now - margin.Milliseconds()}
}
func (w window) meta() map[string]any { return map[string]any{"start_ms": w.start, "end_ms": w.end} }

type fixture struct {
	t    *testing.T
	ctx  context.Context
	rdb  *redis.Client
	repo *StockRepo
	now  int64
}

func newFixture(t *testing.T) *fixture {
	rdb := testkit.OpenRedis(t)
	ctx := context.Background()
	now, err := rdb.Time(ctx).Result()
	require.NoError(t, err)
	return &fixture{t: t, ctx: ctx, rdb: rdb, repo: &StockRepo{redisClient: rdb}, now: now.UnixMilli()}
}

func (f *fixture) meta(aid int64, fields map[string]any) {
	f.t.Helper()
	require.NoError(f.t, f.rdb.HSet(f.ctx, rediskey.Meta(aid), fields).Err())
}

func (f *fixture) stock(aid int64, n int) {
	f.t.Helper()
	require.NoError(f.t, f.rdb.Set(f.ctx, rediskey.Stock(aid), n, 0).Err())
}

func (f *fixture) deduct(aid int64) int {
	f.t.Helper()
	return f.deductAs(aid, "u1")
}

func (f *fixture) deductAs(aid int64, userID string) int {
	f.t.Helper()
	keys := []string{rediskey.Meta(aid), rediskey.Stock(aid), rediskey.Users(aid)}
	got, err := buyScript.Run(f.ctx, f.rdb, keys, userID).Int()
	require.NoError(f.t, err)
	return got
}

func (f *fixture) isBuyer(aid int64, userID string) bool {
	f.t.Helper()
	ok, err := f.rdb.SIsMember(f.ctx, rediskey.Users(aid), userID).Result()
	require.NoError(f.t, err)
	return ok
}

func (f *fixture) bought(aid int64, userIDs ...string) {
	f.t.Helper()
	for _, u := range userIDs {
		require.NoError(f.t, f.rdb.SAdd(f.ctx, rediskey.Users(aid), u).Err())
	}
}

func (f *fixture) assertRedis(aid int64, wantStock int, wantUsers ...string) {
	f.t.Helper()
	left, err := f.rdb.Get(f.ctx, rediskey.Stock(aid)).Int()
	require.NoError(f.t, err)
	assert.Equal(f.t, wantStock, left, "stock")
	users, err := f.rdb.SMembers(f.ctx, rediskey.Users(aid)).Result()
	require.NoError(f.t, err)
	assert.ElementsMatch(f.t, wantUsers, users, "users set")
}

func (f *fixture) sealed(aid int64) bool {
	f.t.Helper()
	n, err := f.rdb.HExists(f.ctx, rediskey.Meta(aid), "sealed").Result()
	require.NoError(f.t, err)
	return n
}

func (f *fixture) assertNoSale(aid int64, wantStock int) {
	f.t.Helper()
	left, err := f.rdb.Get(f.ctx, rediskey.Stock(aid)).Int()
	require.NoError(f.t, err)
	assert.Equal(f.t, wantStock, left, "stock must not move")
	n, err := f.rdb.Exists(f.ctx, rediskey.Users(aid)).Result()
	require.NoError(f.t, err)
	assert.Zero(f.t, n, "users set must not be created")
}

func TestDeduct_Precheck(t *testing.T) {
	f := newFixture(t)

	cases := []struct {
		name  string
		meta  map[string]any
		stock *int
		want  int
	}{
		{"NotPreheated", nil, ptr(10), -1},
		{"MetaMissingEndMs", map[string]any{"start_ms": open(f.now).start}, ptr(10), -1},
		{"NoStockKey", open(f.now).meta(), nil, -4},
		{"NotStarted", notYet(f.now).meta(), ptr(10), -2},
		{"Ended", ended(f.now).meta(), ptr(10), -3},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.t = t
			aid := int64(100 + i)
			if tc.meta != nil {
				f.meta(aid, tc.meta)
			}
			if tc.stock != nil {
				f.stock(aid, *tc.stock)
			}
			assert.Equal(t, tc.want, f.deduct(aid))
		})
	}
}

func TestDeduct_InsideWindowPassesPrecheck(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.stock(aid, 10)

	assert.NotContains(t, []int{-1, -2, -3}, f.deduct(aid))
}

func TestDeduct_NotStartedWritesNothing(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, notYet(f.now).meta())
	f.stock(aid, 10)

	require.Equal(t, -2, f.deduct(aid))
	f.assertNoSale(aid, 10)
	assert.False(t, f.sealed(aid), "only the end side latches")
}

func TestDeduct_EndedSealsAndSellsNothing(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, ended(f.now).meta())
	f.stock(aid, 10)

	require.Equal(t, -3, f.deduct(aid))
	f.assertNoSale(aid, 10)
	assert.True(t, f.sealed(aid), "first request past end must latch sealed")
}

func TestDeduct_SealIsOneWay(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, ended(f.now).meta())
	f.stock(aid, 10)
	require.Equal(t, -3, f.deduct(aid))

	// Simulates Redis clock stepping back / failover to a slower replica.
	f.meta(aid, open(f.now).meta())

	assert.Equal(t, -3, f.deduct(aid), "sealed must survive the window reopening")
	f.assertNoSale(aid, 10)
}

func TestDeduct_ManualSealInsideWindow(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.meta(aid, map[string]any{"sealed": 1})
	f.stock(aid, 10)

	assert.Equal(t, -3, f.deduct(aid), "manual early close shares the sealed field")
	f.assertNoSale(aid, 10)
}

// window_probe.lua calls window_state with an exact now, so the boundary is tested
// on the real Redis Lua engine without a time override in the production script.
//
//go:embed lua/window_probe.lua
var probeSrc string

func TestWindowBoundary(t *testing.T) {
	f := newFixture(t)
	probe := redis.NewScript(windowSrc + "\n" + probeSrc)
	const start, end int64 = 1_000_000, 2_000_000

	cases := []struct {
		name string
		now  int64
		want int
	}{
		{"BeforeStart", start - 1, -2},
		{"AtStart", start, 0},
		{"BeforeEnd", end - 1, 0},
		{"AtEnd", end, -3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := probe.Run(f.ctx, f.rdb, nil, tc.now, start, end).Int()
			require.NoError(t, err)
			assert.Equal(t, tc.want, got, "window is [start, end); now=%d", tc.now)
		})
	}
}

func ptr(n int) *int { return &n }

const passed = 1

func TestDeduct_Duplicate(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.stock(aid, 10)
	f.bought(aid, "u1")

	assert.Equal(t, 2, f.deductAs(aid, "u1"))
	f.assertRedis(aid, 10, "u1")
}

func TestDeduct_DuplicateIsPerUser(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.stock(aid, 10)
	f.bought(aid, "u1")

	assert.Equal(t, passed, f.deductAs(aid, "u2"))
}

func TestDeduct_DuplicateIsPerActivity(t *testing.T) {
	f := newFixture(t)
	const a1, a2 int64 = 1, 2
	for _, aid := range []int64{a1, a2} {
		f.meta(aid, open(f.now).meta())
		f.stock(aid, 10)
	}
	f.bought(a1, "u1")

	assert.Equal(t, passed, f.deductAs(a2, "u1"), "buying in activity 1 must not block activity 2")
}

func TestDeduct_SoldOut(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name  string
		stock int
	}{
		{"Zero", 0},
		{"NegativeIsStillSoldOut", -1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.t = t
			aid := int64(100 + i)
			f.meta(aid, open(f.now).meta())
			f.stock(aid, tc.stock)

			assert.Equal(t, 0, f.deductAs(aid, "u1"))
			f.assertRedis(aid, tc.stock)
		})
	}
}

func TestDeduct_DuplicateBeatsSoldOut(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.stock(aid, 0)
	f.bought(aid, "u1")

	assert.Equal(t, 2, f.deductAs(aid, "u1"), "a buyer who already won should hear 'already bought', not 'sold out'")
}

func TestDeduct_LastUnitPasses(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.stock(aid, 1)

	assert.Equal(t, passed, f.deductAs(aid, "u1"), "stock=1 is not sold out")
}

func (f *fixture) release(aid int64, userID string) int {
	f.t.Helper()
	code, err := f.repo.Release(f.ctx, aid, userID)
	require.NoError(f.t, err)
	return code
}

func TestDeduct_SaleTakesOneTokenAndRecordsBuyer(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.stock(aid, 10)

	require.Equal(t, 1, f.deductAs(aid, "u1"))
	f.assertRedis(aid, 9, "u1")

	require.Equal(t, 2, f.deductAs(aid, "u1"), "the buyer just recorded must now be a duplicate")
	f.assertRedis(aid, 9, "u1")
}

func TestDeduct_LastTokenThenSoldOut(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.stock(aid, 1)

	require.Equal(t, 1, f.deductAs(aid, "u1"))
	require.Equal(t, 0, f.deductAs(aid, "u2"))
	f.assertRedis(aid, 0, "u1")
}

func TestRelease_ReturnsTokenAndLetsUserRetry(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.stock(aid, 1)
	require.Equal(t, 1, f.deductAs(aid, "u1"))

	assert.Equal(t, 1, f.release(aid, "u1"))
	f.assertRedis(aid, 1)

	assert.Equal(t, 1, f.deductAs(aid, "u1"), "after a DB failure the user must be able to try again")
}

func TestRelease_IsIdempotent(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.stock(aid, 10)
	require.Equal(t, 1, f.deductAs(aid, "u1"))

	assert.Equal(t, 1, f.release(aid, "u1"))
	assert.Equal(t, 0, f.release(aid, "u1"), "a retried compensation must not return a second token")
	f.assertRedis(aid, 10)
}

func TestRelease_UnknownBuyerReturnsNothing(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.stock(aid, 10)
	f.bought(aid, "u1")

	assert.Equal(t, 0, f.release(aid, "u2"))
	f.assertRedis(aid, 10, "u1")
}

func TestRelease_DoesNotRecreateLostStock(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.bought(aid, "u1")

	assert.Equal(t, -4, f.release(aid, "u1"), "stock lost must surface to the caller, not look like 'not held'")
	assert.True(t, f.isBuyer(aid, "u1"), "nothing is touched when stock is lost")

	n, err := f.rdb.Exists(f.ctx, rediskey.Stock(aid)).Result()
	require.NoError(t, err)
	assert.Zero(t, n, "INCR on a missing key would create stock=1 and hide the -4 alarm")
}

func TestDeduct_Concurrent(t *testing.T) {
	f := newFixture(t)

	const (
		aid    int64 = 1
		stock  int64 = 100
		buyers int64 = 1000
	)

	f.meta(aid, open(f.now).meta())
	f.stock(aid, int(stock))

	var success, soldOut, other atomic.Int64

	start := make(chan struct{})
	wg := sync.WaitGroup{}
	for uid := range buyers {
		wg.Go(func() {
			<-start
			err := f.repo.DeductStock(f.ctx, aid, "u"+fmt.Sprintf("%d", uid))
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, domain.ErrSoldOut):
				soldOut.Add(1)
			default:
				other.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()

	assert.Zero(t, other.Load())
	assert.Equal(t, stock, success.Load())
	assert.Equal(t, buyers-stock, soldOut.Load())

	remain, err := f.rdb.Get(f.ctx, rediskey.Stock(aid)).Int()
	require.NoError(t, err)
	assert.Equal(t, 0, remain)

	n, err := f.rdb.SCard(f.ctx, rediskey.Users(aid)).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(100), n)
}
