//go:build integration

package redis

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
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

func deductErr(_ int32, err error) error { return err }

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

// openWithSlots preheats an open activity holding slots 1..n and returns them.
func (f *fixture) openWithSlots(aid int64, n int) []int32 {
	f.t.Helper()
	f.meta(aid, open(f.now).meta())
	return f.slots(aid, n)
}

func (f *fixture) slots(aid int64, n int) []int32 {
	f.t.Helper()
	seeded := make([]int32, n)
	members := make([]any, n)
	for i := range n {
		seeded[i] = int32(i + 1)
		members[i] = i + 1
	}
	require.NoError(f.t, f.rdb.SAdd(f.ctx, rediskey.Slots(aid), members...).Err())
	return seeded
}

func (f *fixture) deductAs(aid int64, userID string) (int32, error) {
	f.t.Helper()
	return f.repo.DeductStock(f.ctx, aid, userID)
}

func (f *fixture) bought(aid int64, userIDs ...string) {
	f.t.Helper()
	for _, u := range userIDs {
		require.NoError(f.t, f.rdb.SAdd(f.ctx, rediskey.Users(aid), u).Err())
	}
}

func (f *fixture) slotsLeft(aid int64) []int32 {
	f.t.Helper()
	members, err := f.rdb.SMembers(f.ctx, rediskey.Slots(aid)).Result()
	require.NoError(f.t, err)
	out := make([]int32, len(members))
	for i, m := range members {
		n, err := strconv.ParseInt(m, 10, 32)
		require.NoError(f.t, err)
		out[i] = int32(n)
	}
	return out
}

func (f *fixture) users(aid int64) []string {
	f.t.Helper()
	m, err := f.rdb.SMembers(f.ctx, rediskey.Users(aid)).Result()
	require.NoError(f.t, err)
	return m
}

func (f *fixture) metaHas(aid int64, field string) bool {
	f.t.Helper()
	ok, err := f.rdb.HExists(f.ctx, rediskey.Meta(aid), field).Result()
	require.NoError(f.t, err)
	return ok
}

func (f *fixture) exists(key string) bool {
	f.t.Helper()
	n, err := f.rdb.Exists(f.ctx, key).Result()
	require.NoError(f.t, err)
	return n == 1
}

func (f *fixture) assertNoSale(aid int64, wantSlots []int32) {
	f.t.Helper()
	assert.ElementsMatch(f.t, wantSlots, f.slotsLeft(aid), "slots must not move")
	assert.False(f.t, f.exists(rediskey.Users(aid)), "users set must not be created")
	assert.False(f.t, f.metaHas(aid, "drained"), "drained must not be written")
}

func TestDeduct_Precheck(t *testing.T) {
	f := newFixture(t)

	cases := []struct {
		name       string
		meta       map[string]any
		want       error
		wantSealed bool
	}{
		{"NotPreheated", nil, domain.ErrNotPreheated, false},
		{"MetaMissingEndMs", map[string]any{"start_ms": open(f.now).start}, domain.ErrNotPreheated, false},
		{"NotStarted", notYet(f.now).meta(), domain.ErrNotOpen, false},
		{"EndedLatchesSealed", ended(f.now).meta(), domain.ErrSealed, true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.t = t
			aid := int64(100 + i)
			if tc.meta != nil {
				f.meta(aid, tc.meta)
			}
			seeded := f.slots(aid, 3)

			slot, err := f.deductAs(aid, "u1")
			assert.ErrorIs(t, err, tc.want)
			assert.Zero(t, slot)
			f.assertNoSale(aid, seeded)
			assert.Equal(t, tc.wantSealed, f.metaHas(aid, "sealed"), "only the end side latches")
		})
	}
}

func TestDeduct_SealIsOneWay(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, ended(f.now).meta())
	seeded := f.slots(aid, 3)
	require.ErrorIs(t, deductErr(f.deductAs(aid, "u1")), domain.ErrSealed)

	// Simulates Redis clock stepping back / failover to a slower replica.
	f.meta(aid, open(f.now).meta())

	assert.ErrorIs(t, deductErr(f.deductAs(aid, "u1")), domain.ErrSealed, "sealed must survive the window reopening")
	f.assertNoSale(aid, seeded)
}

func TestDeduct_ManualSealInsideWindow(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	seeded := f.openWithSlots(aid, 3)
	f.meta(aid, map[string]any{"sealed": 1})

	assert.ErrorIs(t, deductErr(f.deductAs(aid, "u1")), domain.ErrSealed, "manual early close shares the sealed field")
	f.assertNoSale(aid, seeded)
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

func TestDeduct_PassHandsOutSlotAndRecordsBuyer(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	seeded := f.openWithSlots(aid, 3)

	slot, err := f.deductAs(aid, "u1")
	require.NoError(t, err)
	assert.Contains(t, seeded, slot)
	assert.NotContains(t, f.slotsLeft(aid), slot, "a handed-out slot must leave the pool")
	assert.Len(t, f.slotsLeft(aid), 2)
	assert.ElementsMatch(t, []string{"u1"}, f.users(aid))
	assert.False(t, f.metaHas(aid, "drained"), "slots remain, so not drained")

	_, err = f.deductAs(aid, "u1")
	assert.ErrorIs(t, err, domain.ErrAlreadyAdmitted, "the buyer just recorded must now be a duplicate")
	assert.Len(t, f.slotsLeft(aid), 2, "a duplicate must not consume a slot")
}

func TestDeduct_EverySlotHandedOutOnce(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	seeded := f.openWithSlots(aid, 5)

	var got []int32
	for i := range seeded {
		slot, err := f.deductAs(aid, fmt.Sprintf("u%d", i))
		require.NoError(t, err)
		got = append(got, slot)
	}
	assert.ElementsMatch(t, seeded, got, "each seeded slot exactly once")

	assert.ErrorIs(t, deductErr(f.deductAs(aid, "late")), domain.ErrSoldOut)
	assert.NotContains(t, f.users(aid), "late")
}

func TestDeduct_LastSlotLatchesDrained(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.openWithSlots(aid, 2)

	require.NoError(t, deductErr(f.deductAs(aid, "u1")))
	assert.False(t, f.metaHas(aid, "drained"))

	require.NoError(t, deductErr(f.deductAs(aid, "u2")))
	assert.True(t, f.metaHas(aid, "drained"), "the SPOP that empties the set must latch drained")
	assert.False(t, f.exists(rediskey.Slots(aid)))

	assert.ErrorIs(t, deductErr(f.deductAs(aid, "u3")), domain.ErrSoldOut)
	assert.ElementsMatch(t, []string{"u1", "u2"}, f.users(aid))
}

// An absent slots key without drained means Redis lost data, not a sell-out.
// The script must write nothing, or a later rebuild would trust a phantom buyer
// or a false drained flag.
func TestDeduct_StockLostWritesNothing(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.bought(aid, "u0")

	slot, err := f.deductAs(aid, "u1")
	assert.ErrorIs(t, err, ErrStockLost)
	assert.Zero(t, slot)
	assert.False(t, f.metaHas(aid, "drained"), "stock lost must not be recorded as sold out")
	assert.ElementsMatch(t, []string{"u0"}, f.users(aid), "user must not be admitted without a slot")
	assert.False(t, f.exists(rediskey.Slots(aid)))
}

func TestDeduct_DrainedWithoutSlotsIsSoldOut(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.meta(aid, map[string]any{"drained": 1})

	assert.ErrorIs(t, deductErr(f.deductAs(aid, "u1")), domain.ErrSoldOut)
	assert.False(t, f.exists(rediskey.Users(aid)))
}

func TestDeduct_Duplicate(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	seeded := f.openWithSlots(aid, 3)
	f.bought(aid, "u1")

	slot, err := f.deductAs(aid, "u1")
	assert.ErrorIs(t, err, domain.ErrAlreadyAdmitted)
	assert.Zero(t, slot)
	assert.ElementsMatch(t, seeded, f.slotsLeft(aid), "no slot consumed")
	assert.ElementsMatch(t, []string{"u1"}, f.users(aid))
}

func TestDeduct_DuplicateIsPerUser(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.openWithSlots(aid, 3)
	f.bought(aid, "u1")

	assert.NoError(t, deductErr(f.deductAs(aid, "u2")))
}

func TestDeduct_DuplicateIsPerActivity(t *testing.T) {
	f := newFixture(t)
	const a1, a2 int64 = 1, 2
	f.openWithSlots(a1, 3)
	f.openWithSlots(a2, 3)
	f.bought(a1, "u1")

	assert.NoError(t, deductErr(f.deductAs(a2, "u1")), "buying in activity 1 must not block activity 2")
}

func TestDeduct_DuplicateBeatsSoldOut(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.meta(aid, open(f.now).meta())
	f.meta(aid, map[string]any{"drained": 1})
	f.bought(aid, "u1")

	assert.ErrorIs(t, deductErr(f.deductAs(aid, "u1")), domain.ErrAlreadyAdmitted,
		"a buyer who already won should hear 'already bought', not 'sold out'")
}

func TestDeduct_Concurrent(t *testing.T) {
	f := newFixture(t)

	const (
		aid    int64 = 1
		slots        = 100
		buyers       = 1000
	)
	seeded := f.openWithSlots(aid, slots)

	var (
		soldOut, other atomic.Int64
		mu             sync.Mutex
		won            []int32
	)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for uid := range buyers {
		wg.Go(func() {
			<-start
			slot, err := f.repo.DeductStock(f.ctx, aid, fmt.Sprintf("u%d", uid))
			switch {
			case err == nil:
				mu.Lock()
				won = append(won, slot)
				mu.Unlock()
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
	assert.ElementsMatch(t, seeded, won, "exactly the seeded slots, each once")
	assert.EqualValues(t, buyers-slots, soldOut.Load())
	assert.True(t, f.metaHas(aid, "drained"))
	assert.Len(t, f.users(aid), slots)
}

func (f *fixture) release(aid int64, slot int32, user string, removed bool) error {
	f.t.Helper()
	return f.repo.Release(f.ctx, aid, slot, user, removed)
}

func TestRelease_DuringSale(t *testing.T) {
	for _, removed := range []bool{true, false} {
		t.Run(fmt.Sprintf("removed=%t", removed), func(t *testing.T) {
			f := newFixture(t)
			const aid int64 = 1
			seeded := f.openWithSlots(aid, 3)
			slot, err := f.deductAs(aid, "u1")
			require.NoError(t, err)

			require.NoError(t, f.release(aid, slot, "u1", removed))
			assert.ElementsMatch(t, seeded, f.slotsLeft(aid), "slot must return to the pool")
			assert.False(t, f.metaHas(aid, "drained"))
			if removed {
				assert.Empty(t, f.users(aid))
			} else {
				assert.ElementsMatch(t, []string{"u1"}, f.users(aid))
			}
		})
	}
}

// Release must read drained as a string flag; comparing it to a number would
// treat a sold-out activity as lost and strand the returned slot.
func TestRelease_AfterSoldOutReopensSale(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.openWithSlots(aid, 1)
	slot, err := f.deductAs(aid, "u1")
	require.NoError(t, err)
	require.True(t, f.metaHas(aid, "drained"))
	require.False(t, f.exists(rediskey.Slots(aid)))

	require.NoError(t, f.release(aid, slot, "u1", true))
	assert.ElementsMatch(t, []int32{slot}, f.slotsLeft(aid))
	assert.False(t, f.metaHas(aid, "drained"), "a returned slot means not sold out")

	got, err := f.deductAs(aid, "u2")
	require.NoError(t, err)
	assert.Equal(t, slot, got)
}

func TestRelease_UserOnly(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	seeded := f.openWithSlots(aid, 3)
	f.bought(aid, "u1", "u2")

	require.NoError(t, f.release(aid, 0, "u1", true))
	assert.ElementsMatch(t, seeded, f.slotsLeft(aid), "slot 0 must not touch the pool")
	assert.ElementsMatch(t, []string{"u2"}, f.users(aid))
}

func TestRelease_StockLostWritesNothing(t *testing.T) {
	for _, slot := range []int32{2, 0} {
		t.Run(fmt.Sprintf("slot=%d", slot), func(t *testing.T) {
			f := newFixture(t)
			const aid int64 = 1
			f.meta(aid, open(f.now).meta())
			f.bought(aid, "u1")

			assert.ErrorIs(t, f.release(aid, slot, "u1", true), ErrStockLost)
			assert.False(t, f.exists(rediskey.Slots(aid)), "SADD on a lost key would hide the loss")
			assert.False(t, f.metaHas(aid, "drained"))
			assert.ElementsMatch(t, []string{"u1"}, f.users(aid))
		})
	}
}

func TestRelease_IsIdempotent(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	seeded := f.openWithSlots(aid, 3)
	slot, err := f.deductAs(aid, "u1")
	require.NoError(t, err)
	f.bought(aid, "u2")

	require.NoError(t, f.release(aid, slot, "u1", true))
	slots, users := f.slotsLeft(aid), f.users(aid)

	require.NoError(t, f.release(aid, slot, "u1", true))
	assert.ElementsMatch(t, slots, f.slotsLeft(aid))
	assert.ElementsMatch(t, users, f.users(aid))
	assert.ElementsMatch(t, seeded, slots)
	assert.ElementsMatch(t, []string{"u2"}, users)
}

func TestRelease_RoundTripLetsUserBuyAgain(t *testing.T) {
	f := newFixture(t)
	const aid int64 = 1
	f.openWithSlots(aid, 1)
	slot, err := f.deductAs(aid, "u1")
	require.NoError(t, err)

	require.NoError(t, f.release(aid, slot, "u1", true))

	again, err := f.deductAs(aid, "u1")
	require.NoError(t, err)
	assert.Equal(t, slot, again)
	assert.ElementsMatch(t, []string{"u1"}, f.users(aid))
}
