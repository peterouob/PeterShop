//go:build integration

package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/pkg/circuit"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fault int

const (
	faultNone fault = iota
	faultCrash
	faultRedisTimeout
	faultCommitTimeout
	faultDBDown
	faultCommitThenCrash
)

// lossy reports whether a fault can lose the buyer's slot.
func lossy(f fault) bool { return f == faultCrash || f == faultRedisTimeout }

var errInjected = errors.New("injected fault")

type faultyRepo struct {
	repository.SeckillRepo
	plan                                   func(userID string) fault
	crashes, redisTimeouts, commitTimeouts atomic.Int64
	dbDowns, commitCrashes                 atomic.Int64
	dbRejects, reserveErrs                 atomic.Int64

	mu sync.Mutex
	// Slots taken by crashed or reply-lost buyers; nothing ever returns them.
	dropped []int32
	// Buyers whose process actually died; a planned crash that never fired
	// still compensates.
	dead map[string]bool
}

func (r *faultyRepo) drop(slot int32) {
	r.mu.Lock()
	r.dropped = append(r.dropped, slot)
	r.mu.Unlock()
}

func (r *faultyRepo) die(uid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dead == nil {
		r.dead = map[string]bool{}
	}
	r.dead[uid] = true
	return errInjected
}

func (r *faultyRepo) DeductStock(ctx context.Context, aid int64, uid string) (int32, error) {
	slot, err := r.SeckillRepo.DeductStock(ctx, aid, uid)
	if err != nil {
		return 0, err
	}
	if r.plan(uid) == faultRedisTimeout {
		r.redisTimeouts.Add(1)
		r.drop(slot)
		return 0, errInjected
	}
	return slot, nil
}

func (r *faultyRepo) Reserve(ctx context.Context, orderNo, aid int64, uid string, slotNo int32) error {
	switch r.plan(uid) {
	case faultCrash:
		r.crashes.Add(1)
		r.drop(slotNo)
		return r.die(uid)
	case faultDBDown:
		r.dbDowns.Add(1)
		return errInjected
	case faultCommitTimeout:
		if err := r.realReserve(ctx, orderNo, aid, uid, slotNo); err != nil {
			return err
		}
		r.commitTimeouts.Add(1)
		return errInjected
	case faultCommitThenCrash:
		if err := r.realReserve(ctx, orderNo, aid, uid, slotNo); err != nil {
			return err
		}
		r.commitCrashes.Add(1)
		return r.die(uid)
	}
	return r.realReserve(ctx, orderNo, aid, uid, slotNo)
}

// Release is the compensation step; a crashed process never reaches it.
func (r *faultyRepo) Release(ctx context.Context, aid int64, slotNo int32, uid string, removed bool) error {
	r.mu.Lock()
	dead := r.dead[uid]
	r.mu.Unlock()
	if dead {
		return nil
	}
	return r.SeckillRepo.Release(ctx, aid, slotNo, uid, removed)
}

func (r *faultyRepo) realReserve(ctx context.Context, orderNo, aid int64, uid string, slotNo int32) error {
	err := r.SeckillRepo.Reserve(ctx, orderNo, aid, uid, slotNo)
	if _, isBiz := errors.AsType[*domain.Error](err); isBiz {
		r.dbRejects.Add(1)
	} else if err != nil {
		r.reserveErrs.Add(1)
	}
	return err
}

func neverOpenGate() *circuit.Gate[struct{}] {
	return circuit.NewGate[struct{}](gobreaker.Settings{
		ReadyToTrip: func(gobreaker.Counts) bool { return false },
	})
}

type bench struct {
	t    *testing.T
	ctx  context.Context
	rdb  *redis.Client
	pool *pgxpool.Pool
	repo *faultyRepo
	svc  *seckillService

	totals map[int64]int
	// Buyers whose plan could lose a slot, per activity. Each faults at most
	// once between rebuilds since the fault leaves them admitted, so this
	// bounds lost without trusting the fault counters.
	lossyBuyers map[int64]map[string]bool
}

func newBench(t *testing.T, aid int64, stock int, plan func(string) fault) *bench {
	t.Helper()
	ctx := context.Background()
	if plan == nil {
		plan = func(_ string) fault {
			return faultNone
		}
	}

	rdb := testkit.OpenRedis(t)
	db := testkit.OpenContainer(t, "seckill")
	require.NoError(t, db.Migrator(t).Up())
	poolCfg, err := pgxpool.ParseConfig(db.DSN)
	require.NoError(t, err)
	if v := os.Getenv("TEST_PG_MAXCONNS"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoError(t, err, "TEST_PG_MAXCONNS must be an integer")
		poolCfg.MaxConns = int32(n)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	repo := &faultyRepo{SeckillRepo: repository.NewSeckillRepo(rdb, pool), plan: plan}
	var seq atomic.Int64
	svc := &seckillService{repo: repo, nextID: func() (int64, error) { return seq.Add(1), nil }, cb: neverOpenGate(), reserveTimeout: reserveTimeout}

	b := &bench{t: t, ctx: ctx, rdb: rdb, pool: pool, repo: repo, svc: svc,
		totals: map[int64]int{}, lossyBuyers: map[int64]map[string]bool{}}
	b.addActivity(aid, stock)
	return b
}

func (b *bench) addActivity(aid int64, stock int) {
	b.t.Helper()
	b.totals[aid] = stock
	b.lossyBuyers[aid] = map[string]bool{}
	b.seedRedis(aid, stock)

	_, err := b.pool.Exec(b.ctx,
		`INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
		 VALUES ($1, 'p1', 100, now() - interval '1 hour', now() + interval '1 hour', 2, now(), now())`, aid)
	require.NoError(b.t, err)
	_, err = b.pool.Exec(b.ctx,
		`INSERT INTO activity_slot (activity_id, slot_no) SELECT $1, g FROM generate_series(1, $2::int) g`,
		aid, stock)
	require.NoError(b.t, err)
}

func (b *bench) seedRedis(aid int64, stock int) {
	b.t.Helper()
	redisNow, err := b.rdb.Time(b.ctx).Result()
	require.NoError(b.t, err)
	require.NoError(b.t, b.rdb.HSet(b.ctx, rediskey.Meta(aid), map[string]any{
		"start_ms": redisNow.Add(-time.Hour).UnixMilli(),
		"end_ms":   redisNow.Add(time.Hour).UnixMilli(),
	}).Err())
	slots := make([]any, stock)
	for i := range slots {
		slots[i] = i + 1
	}
	require.NoError(b.t, b.rdb.SAdd(b.ctx, rediskey.Slots(aid), slots...).Err())
}

type outcome struct {
	orderNo int64
	err     error
}

func (b *bench) buyAll(users []string, aid int64) []outcome {
	for _, u := range users {
		if lossy(b.repo.plan(u)) {
			b.lossyBuyers[aid][u] = true
		}
	}
	results := make([]outcome, len(users))
	ch := make(chan struct{})

	wg := sync.WaitGroup{}
	for i, user := range users {
		wg.Go(func() {
			<-ch
			orderNo, err := b.svc.Buy(b.ctx, aid, user)
			results[i] = outcome{orderNo: orderNo, err: err}
		})
	}
	close(ch)
	wg.Wait()
	return results
}

type snapshot struct {
	redisStock  int
	redisUsers  map[string]bool
	dbAvailable int
	ledger      map[string]int64

	// Released rows free their slot but still block the user, as the
	// user unique key spans every status.
	activeRows int
	dupUser    int
}

func (b *bench) snapshot(aid int64) snapshot {
	b.t.Helper()
	s := snapshot{
		redisUsers: map[string]bool{},
		ledger:     map[string]int64{},
	}

	stock, err := b.rdb.SCard(b.ctx, rediskey.Slots(aid)).Result()
	if err != nil {
		b.t.Fatalf("failed to get stock from redis: %v", err)
	}

	s.redisStock = int(stock)

	members, err := b.rdb.SMembers(b.ctx, rediskey.Users(aid)).Result()
	if err != nil {
		b.t.Fatalf("failed to get members from redis: %v", err)
	}

	for _, user := range members {
		s.redisUsers[user] = true
	}

	err = b.pool.QueryRow(b.ctx,
		`SELECT count(*)
			FROM activity_slot sl
		LEFT JOIN stock_ledger l
			ON l.activity_id = sl.activity_id 
			AND l.slot_no = sl.slot_no
			AND l.status <> 3
		WHERE sl.activity_id = $1
			AND sl.disabled_at is NULL
			AND l.slot_no is null `, aid).
		Scan(&s.dbAvailable)
	if err != nil {
		b.t.Fatalf("failed to get stock from db: %v", err)
	}

	rows, err := b.pool.Query(b.ctx,
		`SELECT user_id, order_no, status <> 3 FROM stock_ledger WHERE activity_id = $1`, aid)
	if err != nil {
		b.t.Fatalf("snapshot: SELECT ledger: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var user string
		var orderNo int64
		var active bool

		if err := rows.Scan(&user, &orderNo, &active); err != nil {
			b.t.Fatalf("snapshot: SELECT ledger: %v", err)
		}
		if active {
			s.activeRows++
		}

		if _, seen := s.ledger[user]; seen {
			s.dupUser++
			continue
		}
		s.ledger[user] = orderNo
	}

	if err := rows.Err(); err != nil {
		b.t.Fatalf("snapshot: SELECT ledger: %v", err)
	}

	return s
}

func (b *bench) assertSafety(aid int64, total int, s snapshot, results []outcome) {
	t := b.t
	t.Helper()
	defer b.dumpOnFail(aid, t.Failed())

	if s.activeRows > total {
		t.Errorf("oversold: %d active ledger rows exceed stock total %d", s.activeRows, total)
	}

	if s.dupUser != 0 {
		t.Errorf("duplicate winner: %d extra ledger rows for users who already won", s.dupUser)
	}

	inLedger := make(map[int64]bool, len(s.ledger))
	for _, orderNo := range s.ledger {
		inLedger[orderNo] = true
	}

	for i, r := range results {
		if r.err == nil && !inLedger[r.orderNo] {
			t.Errorf("reported success but no order: results[%d] order_no=%d not in ledger", i, r.orderNo)
		}
	}
}

// assertRebuilt checks redis converged to db: same free slots, same users,
// and drained set exactly when nothing is free.
func (b *bench) assertRebuilt(aid int64) {
	b.t.Helper()
	defer b.dumpOnFail(aid, b.t.Failed())

	rows, err := b.pool.Query(b.ctx, `
	SELECT sl.slot_no FROM activity_slot sl
	WHERE sl.activity_id = $1 AND sl.disabled_at IS NULL
	  AND NOT EXISTS (SELECT 1 FROM stock_ledger l
	                  WHERE l.activity_id = sl.activity_id AND l.slot_no = sl.slot_no AND l.status <> 3)`, aid)
	require.NoError(b.t, err)
	dbFree, err := pgx.CollectRows(rows, pgx.RowTo[int32])
	require.NoError(b.t, err)
	assert.ElementsMatch(b.t, dbFree, slices.Collect(maps.Keys(b.slotSets(aid).free)), "redis free slots == db free slots")

	s := b.snapshot(aid)
	ledgerUsers := make(map[string]bool, len(s.ledger))
	for u := range s.ledger {
		ledgerUsers[u] = true
	}
	assert.Equal(b.t, ledgerUsers, s.redisUsers, "redis users == ledger users")

	drained, err := b.rdb.HGet(b.ctx, rediskey.Meta(aid), "drained").Result()
	if !errors.Is(err, redis.Nil) {
		require.NoError(b.t, err)
	}
	assert.Equal(b.t, len(dbFree) == 0, drained == "1", "drained flag set iff db has no free slot")
}

func (b *bench) dumpOnFail(aid int64, failedBefore bool) {
	if failedBefore || !b.t.Failed() {
		return
	}
	s := b.slotSets(aid)
	b.repo.mu.Lock()
	dropped := slices.Sorted(slices.Values(b.repo.dropped))
	b.repo.mu.Unlock()
	r := b.repo
	b.t.Logf("state dump activity=%d total=%d\n R=%v\n L=%v (rows=%d)\n dup=%v lost=%d lossy buyers=%d\n dropped=%v\n"+
		" faults: crash=%d redisTimeout=%d commitTimeout=%d commitThenCrash=%d dbDown=%d dbRejects=%d reserveErrs=%d",
		aid, b.totals[aid], slices.Sorted(maps.Keys(s.free)), slices.Sorted(maps.Keys(s.active)), s.activeRows,
		s.dup(), s.lost(b.totals[aid]), len(b.lossyBuyers[aid]), dropped,
		r.crashes.Load(), r.redisTimeouts.Load(), r.commitTimeouts.Load(), r.commitCrashes.Load(), r.dbDowns.Load(), r.dbRejects.Load(), r.reserveErrs.Load())
}

func userIDs(n int) []string { return userIDsFrom(0, n) }

func userIDsFrom(start, n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("u%d", start+i)
	}
	return ids
}

func everyNth(n int, f fault) func(string) fault {
	return func(uid string) fault {
		i, err := strconv.Atoi(strings.TrimPrefix(uid, "u"))
		if err == nil && i%n == 0 {
			return f
		}
		return faultNone
	}
}

func repeatUser(uid string, n int) []string {
	users := make([]string, n)
	for i := range users {
		users[i] = uid
	}
	return users
}

func countErrs(results []outcome, target error) int {
	n := 0
	for _, r := range results {
		if errors.Is(r.err, target) {
			n++
		}
	}
	return n
}

func cycleFaults(faults ...fault) func(string) fault {
	return func(uid string) fault {
		i, err := strconv.Atoi(strings.TrimPrefix(uid, "u"))
		if err != nil {
			return faultNone
		}
		return faults[i%len(faults)]
	}
}

type slotSets struct {
	free       map[int32]bool
	active     map[int32]bool
	activeRows int
}

func (b *bench) slotSets(aid int64) slotSets {
	b.t.Helper()
	s := slotSets{
		free:   make(map[int32]bool),
		active: make(map[int32]bool)}

	res, err := b.rdb.SMembers(b.ctx, rediskey.Slots(aid)).Result()
	if err != nil {
		b.t.Fatalf("slotSets: read redis slots: %v", err)
	}

	for _, r := range res {
		id, err := strconv.ParseInt(r, 10, 32)
		if err != nil {
			b.t.Fatalf("slotSets: parse redis slot %q: %v", r, err)
		}
		s.free[int32(id)] = true
	}

	rows, err := b.pool.Query(b.ctx, `
	SELECT slot_no FROM stock_ledger WHERE activity_id = $1 AND status <> 3`,
		aid)
	if err != nil {
		b.t.Fatalf("slotSets: query ledger slots: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var slotNo int32
		if err := rows.Scan(&slotNo); err != nil {
			b.t.Fatalf("slotSets: scan ledger slot: %v", err)
		}
		s.active[slotNo] = true
		s.activeRows++
	}

	if err := rows.Err(); err != nil {
		b.t.Fatalf("slotSets: iterate ledger slots: %v", err)
	}

	return s
}

func (s slotSets) dup() []int32 {
	var out []int32
	for i := range s.free {
		if s.active[i] {
			out = append(out, i)
		}
	}

	slices.Sort(out)
	return out
}

func (s slotSets) lost(total int) int {
	union := len(s.free) + len(s.active) - len(s.dup())
	return total - union
}

func (b *bench) assertSlots(aid int64, total, wantLost int) {
	b.t.Helper()
	defer b.dumpOnFail(aid, b.t.Failed())

	s := b.slotSets(aid)
	if s.activeRows != len(s.active) {
		b.t.Errorf("%d active ledger rows share only %d slots", s.activeRows, len(s.active))
	}

	checkRange := func(name string, mp map[int32]bool) {
		for n := range mp {
			if n < 1 || int(n) > total {
				b.t.Errorf("%s holds slot %d outside 1..%d", name, n, total)
			}
		}
	}
	checkRange("redis free set", s.free)
	checkRange("active ledger", s.active)

	lost := s.lost(total)
	if lost != wantLost {
		b.t.Errorf("lost slot number error, want: %d, got: %d", wantLost, lost)
	}
	if bound := len(b.lossyBuyers[aid]); lost < 0 || lost > bound {
		b.t.Errorf("lost %d outside 0..%d (buyers planned to crash or lose the redis reply)", lost, bound)
	}

	b.t.Logf("active: %d, free: %d, dup: %d, lost: %d", len(s.active), len(s.free), len(s.dup()), lost)

	for _, id := range s.dup() {
		oid, err := b.svc.nextID()
		if err != nil {
			b.t.Fatalf("nextID: %v", err)
		}
		err = b.repo.SeckillRepo.Reserve(b.ctx, oid, aid, fmt.Sprintf("reissue-%d", id), id)
		if !errors.Is(err, domain.ErrSlotTaken) {
			b.t.Errorf("slot %d is in redis and ledger, reissue not blocked: err=%v", id, err)
		}
	}
}
