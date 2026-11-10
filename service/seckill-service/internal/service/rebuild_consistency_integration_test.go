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
