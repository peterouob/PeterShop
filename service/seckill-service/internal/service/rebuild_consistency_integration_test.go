//go:build integration

package service

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	redisrepo "github.com/peterouob/seckill_service/service/seckill-service/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsistency_RebuildAfterRedisLoss(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 100, 50
	b := newBench(t, aid, stock, nil)
	first := b.buyAll(userIDs(buyers), aid)
	require.NoError(t, b.rdb.Del(b.ctx, rediskey.Meta(aid), rediskey.Stock(aid), rediskey.Users(aid)).Err())

	_, err := b.svc.Buy(b.ctx, aid, "late")
	require.ErrorIs(t, err, domain.ErrNotPreheated, "with Redis gone the sale stops instead of selling blind")

	require.NoError(t, repository.NewRebuilder(b.rdb, b.pool).Rebuild(b.ctx, aid))
	assertRebuilt(t, b.snapshot(aid))

	again := b.buyAll(userIDs(buyers), aid)
	for _, r := range again {
		assert.ErrorIs(t, r.err, domain.ErrAlreadyAdmitted)
	}
	fresh := b.buyAll(userIDsFrom(buyers, 20), aid)
	for _, r := range fresh {
		assert.NoError(t, r.err)
	}

	s := b.snapshot(aid)
	assertInvariants(t, stock, s, append(append(first, again...), fresh...))
	assertDBLedger(t, s)
	assert.Len(t, s.ledger, buyers+20)
}

func TestConsistency_RebuildDuringSale(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 200, 1000
	b := newBench(t, aid, stock, nil)
	rb := repository.NewRebuilder(b.rdb, b.pool)

	done := make(chan []outcome)
	go func() { done <- b.buyAll(userIDs(buyers), aid) }()

	var results []outcome
	rebuilds := 0
	for results == nil {
		select {
		case results = <-done:
		default:
			require.NoError(t, rb.Rebuild(b.ctx, aid))
			rebuilds++
		}
	}
	t.Logf("rebuilds during the sale: %d", rebuilds)
	require.Positive(t, rebuilds)

	s := b.snapshot(aid)
	assertSafety(t, stock, s, results)
	assertDBLedger(t, s)

	require.NoError(t, rb.Rebuild(b.ctx, aid))
	assertRebuilt(t, b.snapshot(aid))
}

func TestConsistency_ConcurrentRebuilds(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers, rebuilders = 1, 200, 1000, 4
	b := newBench(t, aid, stock, nil)
	rb := repository.NewRebuilder(b.rdb, b.pool)

	done := make(chan struct{})
	var results []outcome
	go func() { results = b.buyAll(userIDs(buyers), aid); close(done) }()

	var ok, mismatched atomic.Int64
	var wg sync.WaitGroup
	for range rebuilders {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				err := rb.Rebuild(b.ctx, aid)
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, redisrepo.ErrRebuildMismatch):
					mismatched.Add(1)
				default:
					t.Errorf("overlapping rebuild failed with something other than a mismatch: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
	t.Logf("rebuilds ok=%d aborted by mismatch=%d", ok.Load(), mismatched.Load())
	require.Positive(t, ok.Load()+mismatched.Load())

	s := b.snapshot(aid)
	assertSafety(t, stock, s, results)
	assertDBLedger(t, s)

	require.NoError(t, rb.Rebuild(b.ctx, aid))
	assertRebuilt(t, b.snapshot(aid))
}

func TestConsistency_RebuildFreesCrashedBuyers(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 30, 30
	b := newBench(t, aid, stock, everyNth(3, faultCrash))
	first := b.buyAll(userIDs(buyers), aid)
	require.Positive(t, b.repo.crashes.Load(), "the fault plan must crash some buyers before Reserve")
	crashes := int(b.repo.crashes.Load())

	var crashed []string
	for _, u := range userIDs(buyers) {
		if b.repo.plan(u) == faultCrash {
			crashed = append(crashed, u)
		}
	}
	require.Len(t, crashed, crashes, "every crash maps to one planned user")

	before := b.snapshot(aid)
	require.Zero(t, before.redisStock, "redis handed out every token")
	require.Equal(t, crashes, before.dbAvailable, "crashed tokens are lost: db still holds them while redis is empty")

	stuck := b.buyAll(crashed, aid)
	require.Equal(t, len(crashed), countErrs(stuck, domain.ErrAlreadyAdmitted), "crashed buyers are stuck as admitted without an order")

	require.NoError(t, repository.NewRebuilder(b.rdb, b.pool).Rebuild(b.ctx, aid))
	assertRebuilt(t, b.snapshot(aid))

	b.repo.plan = func(string) fault { return faultNone }
	retry := b.buyAll(crashed, aid)
	for _, r := range retry {
		assert.NoError(t, r.err, "rebuild must free crashed buyers to retry")
	}

	s := b.snapshot(aid)
	assert.Len(t, s.ledger, stock, "lost tokens recovered: every unit is sold")
	assert.Zero(t, s.dbAvailable, "no undersell after recovery")
	assertInvariants(t, stock, s, append(append(first, stuck...), retry...))
	assertDBLedger(t, s)
}

func TestConsistency_StockKeyLostStopsSale(t *testing.T) {
	t.Parallel()
	const aid, stock, early = 1, 20, 10
	b := newBench(t, aid, stock, nil)
	first := b.buyAll(userIDs(early), aid)
	for _, r := range first {
		require.NoError(t, r.err)
	}
	require.NoError(t, b.rdb.Del(b.ctx, rediskey.Stock(aid)).Err())

	_, err := b.svc.Buy(b.ctx, aid, "late")
	require.ErrorIs(t, err, redisrepo.ErrStockLost, "a missing stock key stops the sale instead of reading as sold out or unlimited")
	var ledgerRows int
	require.NoError(t, b.pool.QueryRow(b.ctx,
		`SELECT count(*) FROM stock_ledger WHERE activity_id = $1`, aid).Scan(&ledgerRows))
	require.Equal(t, early, ledgerRows, "a lost stock key must not reach the db")

	require.NoError(t, repository.NewRebuilder(b.rdb, b.pool).Rebuild(b.ctx, aid))
	assertRebuilt(t, b.snapshot(aid))

	fresh := b.buyAll(userIDsFrom(early, stock-early), aid)
	for _, r := range fresh {
		assert.NoError(t, r.err, "rebuild restores the remaining stock for new buyers")
	}

	s := b.snapshot(aid)
	assert.Len(t, s.ledger, stock, "every unit sold exactly once across the key loss")
	assertInvariants(t, stock, s, append(first, fresh...))
	assertDBLedger(t, s)
}
