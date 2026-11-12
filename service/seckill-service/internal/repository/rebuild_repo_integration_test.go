//go:build integration

package repository

import (
	"context"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/postgresql"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/redis"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const aid int64 = 1

type rebuildEnv struct {
	t     *testing.T
	ctx   context.Context
	pool  *pgxpool.Pool
	rdb   *goredis.Client
	rb    *Rebuilder
	stock *redis.StockRepo
}

func newRebuildEnv(t *testing.T, slots int) *rebuildEnv {
	t.Helper()
	ctx := context.Background()
	db := testkit.OpenContainer(t, "seckill")
	require.NoError(t, db.Migrator(t).Up())
	pool, err := pgxpool.New(ctx, db.DSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	rdb := testkit.OpenRedis(t)

	e := &rebuildEnv{t: t, ctx: ctx, pool: pool, rdb: rdb, rb: NewRebuilder(rdb, pool), stock: redis.NewStockRepo(rdb)}
	e.exec(`INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
		VALUES ($1, 'p1', 100, now() - interval '1 hour', now() + interval '1 hour', 2, now(), now())`, aid)
	e.exec(`INSERT INTO activity_slot (activity_id, slot_no) SELECT $1, g FROM generate_series(1, $2::int) g`, aid, slots)
	return e
}

func (e *rebuildEnv) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.pool.Exec(e.ctx, sql, args...)
	require.NoError(e.t, err)
}

func (e *rebuildEnv) hold(orderNo int64, user string, slot int32) {
	e.t.Helper()
	require.NoError(e.t, postgresql.NewReserveRepo(e.pool).Reserve(e.ctx, orderNo, aid, user, slot))
}

func (e *rebuildEnv) setStatus(orderNo int64, status int) {
	e.t.Helper()
	e.exec(`UPDATE stock_ledger SET status = $2 WHERE order_no = $1`, orderNo, status)
}

func (e *rebuildEnv) rebuild() {
	e.t.Helper()
	require.NoError(e.t, e.rb.Rebuild(e.ctx, aid))
}

func (e *rebuildEnv) redisSlots() []int32 {
	e.t.Helper()
	m, err := e.rdb.SMembers(e.ctx, rediskey.Slots(aid)).Result()
	require.NoError(e.t, err)
	out := make([]int32, len(m))
	for i, s := range m {
		n, err := strconv.ParseInt(s, 10, 32)
		require.NoError(e.t, err)
		out[i] = int32(n)
	}
	return out
}

func (e *rebuildEnv) redisUsers() []string {
	e.t.Helper()
	m, err := e.rdb.SMembers(e.ctx, rediskey.Users(aid)).Result()
	require.NoError(e.t, err)
	return m
}

func (e *rebuildEnv) drained() bool {
	e.t.Helper()
	ok, err := e.rdb.HExists(e.ctx, rediskey.Meta(aid), "drained").Result()
	require.NoError(e.t, err)
	return ok
}

func (e *rebuildEnv) heldSlots() []int32 {
	e.t.Helper()
	rows, err := e.pool.Query(e.ctx, `SELECT slot_no FROM stock_ledger WHERE activity_id = $1 AND status <> 3`, aid)
	require.NoError(e.t, err)
	var out []int32
	for rows.Next() {
		var s int32
		require.NoError(e.t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(e.t, rows.Err())
	return out
}

// lost counts slots neither free in Redis nor held in the ledger: N - |R ∪ L|.
func (e *rebuildEnv) lost(total int) int {
	e.t.Helper()
	seen := map[int32]bool{}
	for _, s := range e.redisSlots() {
		seen[s] = true
	}
	for _, s := range e.heldSlots() {
		require.False(e.t, seen[s], "slot %d is both free in Redis and held in the ledger", s)
		seen[s] = true
	}
	return total - len(seen)
}

func TestRebuild_SlotsAreFreeEnabledAndUnheld(t *testing.T) {
	e := newRebuildEnv(t, 6)
	e.hold(1, "locked", 1)
	e.hold(2, "sold", 2)
	e.setStatus(2, 2)
	e.hold(3, "released", 3)
	e.setStatus(3, 3)
	e.exec(`UPDATE activity_slot SET disabled_at = now() WHERE activity_id = $1 AND slot_no = 5`, aid)
	require.NoError(t, e.rdb.SAdd(e.ctx, rediskey.Slots(aid), 1, 2, 99).Err())
	require.NoError(t, e.rdb.HSet(e.ctx, rediskey.Meta(aid), "drained", 1).Err())

	e.rebuild()

	assert.ElementsMatch(t, []int32{3, 4, 6}, e.redisSlots())
	assert.ElementsMatch(t, []string{"locked", "sold", "released"}, e.redisUsers())
	assert.False(t, e.drained())
}

func TestRebuild_NoFreeSlotsIsSoldOutNotLost(t *testing.T) {
	e := newRebuildEnv(t, 2)
	e.hold(1, "u1", 1)
	e.hold(2, "u2", 2)

	e.rebuild()

	n, err := e.rdb.Exists(e.ctx, rediskey.Slots(aid)).Result()
	require.NoError(t, err)
	assert.Zero(t, n, "an empty slots set is not stored")
	assert.True(t, e.drained())
	_, err = e.stock.DeductStock(e.ctx, aid, "late")
	assert.ErrorIs(t, err, domain.ErrSoldOut)
}

func TestRebuild_RefillsAfterDrained(t *testing.T) {
	e := newRebuildEnv(t, 2)
	e.hold(1, "u1", 1)
	e.hold(2, "u2", 2)
	e.rebuild()
	require.True(t, e.drained())
	e.setStatus(2, 3)

	e.rebuild()

	assert.False(t, e.drained())
	assert.ElementsMatch(t, []int32{2}, e.redisSlots())
	_, err := e.stock.DeductStock(e.ctx, aid, "late")
	assert.NoError(t, err)
}

func TestRebuild_ReleaseDuringRebuildUndersellsUntilRerun(t *testing.T) {
	const total = 4
	e := newRebuildEnv(t, total)
	e.hold(1, "u1", 1)
	e.hold(2, "u2", 2)
	e.rebuild()
	require.Zero(t, e.lost(total))

	snap, err := postgresql.NewSnapshotRepo(e.pool).LoadSnapshot(e.ctx, aid)
	require.NoError(t, err)
	e.setStatus(2, 3)
	require.NoError(t, e.stock.Release(e.ctx, aid, 2, "u2", false))
	require.NoError(t, e.stock.Restore(e.ctx, snap))

	assert.ElementsMatch(t, []int32{3, 4}, e.redisSlots(), "the swap overwrites the release")
	assert.Equal(t, 1, e.lost(total), "undersell only, never oversell")

	e.rebuild()

	assert.ElementsMatch(t, []int32{2, 3, 4}, e.redisSlots())
	assert.Zero(t, e.lost(total))
}
