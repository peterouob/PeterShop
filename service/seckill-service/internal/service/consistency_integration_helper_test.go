//go:build integration

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type fault int

const (
	faultNone fault = iota
	faultCrash
	faultRedisTimeout
	faultCommitTimeout
)

var errInjected = errors.New("injected fault")

type faultyRepo struct {
	repository.SeckillRepo
	plan                                   func(userID string) fault
	crashes, redisTimeouts, commitTimeouts atomic.Int64
	dbRejects                              atomic.Int64
}

func (r *faultyRepo) DeductStock(ctx context.Context, aid int64, uid string) error {
	if err := r.SeckillRepo.DeductStock(ctx, aid, uid); err != nil {
		return err
	}
	if r.plan(uid) == faultRedisTimeout {
		r.redisTimeouts.Add(1)
		return errInjected
	}
	return nil
}

func (r *faultyRepo) Reserve(ctx context.Context, orderNo, aid int64, uid string) error {
	switch r.plan(uid) {
	case faultCrash:
		r.crashes.Add(1)
		return errInjected
	case faultCommitTimeout:
		if err := r.realReserve(ctx, orderNo, aid, uid); err != nil {
			return err
		}
		r.commitTimeouts.Add(1)
		return errInjected
	}
	return r.realReserve(ctx, orderNo, aid, uid)
}

func (r *faultyRepo) realReserve(ctx context.Context, orderNo, aid int64, uid string) error {
	err := r.SeckillRepo.Reserve(ctx, orderNo, aid, uid)
	if _, isBiz := errors.AsType[*domain.Error](err); isBiz {
		r.dbRejects.Add(1)
	}
	return err
}

type bench struct {
	t    *testing.T
	ctx  context.Context
	rdb  *redis.Client
	pool *pgxpool.Pool
	repo *faultyRepo
	svc  *seckillService
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
	svc := &seckillService{repo: repo, nextID: func() (int64, error) { return seq.Add(1), nil }}

	b := &bench{t: t, ctx: ctx, rdb: rdb, pool: pool, repo: repo, svc: svc}
	b.addActivity(aid, stock)
	return b
}

func (b *bench) addActivity(aid int64, stock int) {
	b.t.Helper()
	b.seedRedis(aid, stock)

	_, err := b.pool.Exec(b.ctx,
		`INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
		 VALUES ($1, 'p1', 100, now() - interval '1 hour', now() + interval '1 hour', 2, now(), now())`, aid)
	require.NoError(b.t, err)
	_, err = b.pool.Exec(b.ctx,
		`INSERT INTO activity_stock (activity_id, total, available, locked, sold) VALUES ($1, $2, $2, 0, 0)`,
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
	require.NoError(b.t, b.rdb.Set(b.ctx, rediskey.Stock(aid), stock, 0).Err())
}

type outcome struct {
	orderNo int64
	err     error
}

func (b *bench) buyAll(users []string, aid int64) []outcome {
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
	outbox      map[int64]bool

	dbLocked, dbSold         int
	ledgerLocked, ledgerSold int

	dupUser int
}

func (b *bench) snapshot(aid int64) snapshot {
	b.t.Helper()
	s := snapshot{
		redisUsers: map[string]bool{},
		ledger:     map[string]int64{},
		outbox:     map[int64]bool{},
	}

	stock, err := b.rdb.Get(b.ctx, rediskey.Stock(aid)).Int()
	if err != nil {
		b.t.Fatalf("failed to get stock from redis: %v", err)
	}

	s.redisStock = stock

	members, err := b.rdb.SMembers(b.ctx, rediskey.Users(aid)).Result()
	if err != nil {
		b.t.Fatalf("failed to get members from redis: %v", err)
	}

	for _, user := range members {
		s.redisUsers[user] = true
	}

	err = b.pool.QueryRow(b.ctx,
		`SELECT available, locked, sold FROM activity_stock WHERE activity_id = $1`, aid).
		Scan(&s.dbAvailable, &s.dbLocked, &s.dbSold)
	if err != nil {

		b.t.Fatalf("failed to get stock from db: %v", err)
	}

	rows, err := b.pool.Query(b.ctx,
		`SELECT user_id, order_no FROM stock_ledger WHERE activity_id = $1`, aid)
	if err != nil {
		b.t.Fatalf("snapshot: SELECT ledger: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var user string
		var orderNo int64

		if err := rows.Scan(&user, &orderNo); err != nil {
			b.t.Fatalf("snapshot: SELECT ledger: %v", err)
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

	err = b.pool.QueryRow(b.ctx,
		`SELECT count(*) FILTER (WHERE status = 1), count(*) FILTER (WHERE status = 2)
		 FROM stock_ledger WHERE activity_id = $1`, aid).Scan(&s.ledgerLocked, &s.ledgerSold)
	if err != nil {
		b.t.Fatalf("snapshot: count ledger by status: %v", err)
	}

	outboxRows, err := b.pool.Query(b.ctx,
		`SELECT order_no FROM outbox WHERE (payload->>'activity_id')::bigint = $1`, aid)
	if err != nil {
		b.t.Fatalf("snapshot: SELECT outbox: %v", err)
	}
	orderNos, err := pgx.CollectRows(outboxRows, pgx.RowTo[int64])
	if err != nil {
		b.t.Fatalf("snapshot: SELECT outbox: %v", err)
	}
	for _, no := range orderNos {
		s.outbox[no] = true
	}

	return s
}

func (b *bench) assertAccounting(total int, s snapshot) {
	b.t.Helper()

	redisDeducted := total - s.redisStock
	ledgerRows := len(s.ledger) + s.dupUser
	crashes := int(b.repo.crashes.Load())
	redisTimeouts := int(b.repo.redisTimeouts.Load())
	dbRejects := int(b.repo.dbRejects.Load())

	b.t.Logf("redis deducted=%d ledger=%d crashed before reserve=%d redis reply lost=%d rejected by db=%d",
		redisDeducted, ledgerRows, crashes, redisTimeouts, dbRejects)

	if accounted := ledgerRows + crashes + redisTimeouts + dbRejects; redisDeducted != accounted {
		b.t.Errorf("redis deducted %d but only %d are accounted for "+
			"(ledger=%d, crashed before reserve=%d, redis reply lost=%d, rejected by db=%d)",
			redisDeducted, accounted, ledgerRows, crashes, redisTimeouts, dbRejects)
	}
}

func assertInvariants(t *testing.T, total int, s snapshot, results []outcome) {
	t.Helper()

	rows := len(s.ledger) + s.dupUser
	if rows > total {
		t.Errorf("oversold: %d ledger rows exceed stock total %d", rows, total)
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

	for user, orderNo := range s.ledger {
		if !s.outbox[orderNo] {
			t.Errorf("order %d reserved for %q without an outbox event (relay will never create the order)", orderNo, user)
		}
	}
	for orderNo := range s.outbox {
		if !inLedger[orderNo] {
			t.Errorf("outbox event %d has no ledger row (an order would be created for stock never locked)", orderNo)
		}
	}

	for user := range s.ledger {
		if !s.redisUsers[user] {
			t.Errorf("ledger user %q missing from redis admitted set", user)
		}
	}

	if s.redisStock > s.dbAvailable {
		t.Errorf("redis stock %d above db available %d (redis must never hold more than db)", s.redisStock, s.dbAvailable)
	}
}

func assertDBLedger(t *testing.T, s snapshot) {
	t.Helper()
	if s.dbLocked != s.ledgerLocked {
		t.Errorf("stock counter locked=%d but ledger has %d locked rows (a counter moved without its ledger row, or the reverse)",
			s.dbLocked, s.ledgerLocked)
	}
	if s.dbSold != s.ledgerSold {
		t.Errorf("stock counter sold=%d but ledger has %d sold rows (a counter moved without its ledger row, or the reverse)",
			s.dbSold, s.ledgerSold)
	}
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
