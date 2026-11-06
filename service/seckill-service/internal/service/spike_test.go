//go:build integration && spike

package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type timedRepo struct {
	repository.SeckillRepo
	mu       sync.Mutex
	reserves []time.Duration
}

func (r *timedRepo) Reserve(ctx context.Context, orderNo, aid int64, uid string) error {
	start := time.Now()
	err := r.SeckillRepo.Reserve(ctx, orderNo, aid, uid)
	d := time.Since(start)
	r.mu.Lock()
	r.reserves = append(r.reserves, d)
	r.mu.Unlock()
	return err
}

func (b *bench) timeReserves() *timedRepo {
	tr := &timedRepo{SeckillRepo: b.svc.repo}
	b.svc.repo = tr
	return tr
}

func (b *bench) buyAllTimed(users []string, aid int64) ([]outcome, []time.Duration, time.Duration) {
	results := make([]outcome, len(users))
	took := make([]time.Duration, len(users))
	gate := make(chan struct{})

	var wg sync.WaitGroup
	for i, user := range users {
		wg.Go(func() {
			<-gate
			start := time.Now()
			orderNo, err := b.svc.Buy(b.ctx, aid, user)
			took[i] = time.Since(start)
			results[i] = outcome{orderNo: orderNo, err: err}
		})
	}
	start := time.Now()
	close(gate)
	wg.Wait()
	return results, took, time.Since(start)
}

func logLatency(t *testing.T, name string, ds []time.Duration) {
	t.Helper()
	if len(ds) == 0 {
		t.Logf("%s: no samples", name)
		return
	}
	s := slices.Clone(ds)
	slices.Sort(s)
	at := func(p float64) time.Duration { return s[int(p*float64(len(s)-1))] }
	t.Logf("%s: n=%d p50=%v p99=%v max=%v", name, len(s), at(0.50), at(0.99), s[len(s)-1])
}

func logOutcomes(t *testing.T, results []outcome) {
	t.Helper()
	won := countErrs(results, nil)
	soldOut := countErrs(results, domain.ErrSoldOut)
	admitted := countErrs(results, domain.ErrAlreadyAdmitted)
	timedOut := countErrs(results, context.DeadlineExceeded)
	other := len(results) - won - soldOut - admitted - timedOut
	t.Logf("outcomes: won=%d sold_out=%d already_admitted=%d reserve_timeout=%d other=%d",
		won, soldOut, admitted, timedOut, other)
	for _, r := range results {
		if r.err != nil && !errors.Is(r.err, domain.ErrSoldOut) &&
			!errors.Is(r.err, domain.ErrAlreadyAdmitted) && !errors.Is(r.err, context.DeadlineExceeded) {
			t.Logf("first 'other' error: %v", r.err)
			return
		}
	}
}

func runWinners(t *testing.T, b *bench, aid int64, stock, buyers int) {
	t.Helper()
	tr := b.timeReserves()
	t.Logf("stock=%d buyers=%d pg pool max conns=%d reserveTimeout=%v",
		stock, buyers, b.pool.Config().MaxConns, reserveTimeout)

	results, took, wall := b.buyAllTimed(userIDs(buyers), aid)

	logOutcomes(t, results)
	logLatency(t, "reserve (pool wait + tx)", tr.reserves)
	logLatency(t, "buy (end to end)", took)
	won := countErrs(results, nil)
	t.Logf("wall=%v throughput=%.0f orders/s", wall, float64(won)/wall.Seconds())

	s := b.snapshot(aid)
	assertInvariants(t, stock, s, results)
	assertDBLedger(t, s)
	t.Logf("undersold by reserve timeouts: %d (redis deducted %d, ledger %d)",
		(stock-s.redisStock)-len(s.ledger), stock-s.redisStock, len(s.ledger))
	assert.GreaterOrEqual(t, len(s.ledger), won, "every reported win must be in the ledger")
	assert.LessOrEqual(t, len(s.ledger), stock, "never more orders than stock")
}

// TODO: this will let the system crash
func TestSpike_CrowdLargeStock(t *testing.T) {
	const aid, stock, buyers = 1, 5000, 100_000
	runWinners(t, newBench(t, aid, stock, nil), aid, stock, buyers)
}

func TestSpike_OutboxBacklog(t *testing.T) {
	const aid, stock, buyers, backlog = 1, 2000, 2000, 1_000_000
	b := newBench(t, aid, stock, nil)

	start := time.Now()
	_, err := b.pool.Exec(b.ctx,
		`INSERT INTO outbox (order_no, payload)
		 SELECT g, '{}'::jsonb FROM generate_series(1000000000000::bigint, 1000000000000::bigint + $1 - 1) g`, backlog)
	require.NoError(t, err)
	_, err = b.pool.Exec(b.ctx, `ANALYZE outbox`)
	require.NoError(t, err)
	t.Logf("outbox backlog=%d seeded in %v", backlog, time.Since(start))

	runWinners(t, b, aid, stock, buyers)
}
