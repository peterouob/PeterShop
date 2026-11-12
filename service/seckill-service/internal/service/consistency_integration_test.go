//go:build integration

package service

import (
	"context"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsistency_BenchSmoke(t *testing.T) {
	t.Parallel()
	newBench(t, 1, 10, nil)
}

func TestConsistency_CrashBeforeReserveAndRetry(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 30, 40
	b := newBench(t, aid, stock, everyNth(3, faultCrash))

	results := b.buyAll(userIDs(buyers), aid)
	s := b.snapshot(aid)

	crashes := int(b.repo.crashes.Load())
	require.Positive(t, crashes, "no crash was injected, the scenario proves nothing")
	b.assertSafety(aid, stock, s, results)
	b.assertSlots(aid, stock, crashes)

	require.Equal(t, len(s.ledger), countErrs(results, nil), "without commit timeouts, every ledger row must be a reported success")

	retry := b.buyAll(userIDs(buyers), aid)
	assert.Equal(t, stock, countErrs(retry, domain.ErrAlreadyAdmitted),
		"crashed buyers stay admitted without an order until a rebuild, never 'already bought'")
}

func TestConsistency_RedisTimeoutThenRetry(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 30, 40
	b := newBench(t, aid, stock, everyNth(3, faultRedisTimeout))

	result := b.buyAll(userIDs(buyers), aid)
	s := b.snapshot(aid)

	timeout := int(b.repo.redisTimeouts.Load())
	require.Positive(t, timeout, "no timeout was injected, the scenario proves nothing")
	b.assertSafety(aid, stock, s, result)
	b.assertSlots(aid, stock, timeout)

	retry := b.buyAll(userIDs(buyers), aid)
	assert.Equal(t, stock, countErrs(retry, domain.ErrAlreadyAdmitted),
		"users whose redis reply was lost are told 'already accepted', never 'already bought'")
}

func TestConsistency_CommitTimeout(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 30, 55
	b := newBench(t, aid, stock, everyNth(3, faultCommitTimeout))

	result := b.buyAll(userIDs(buyers), aid)
	s := b.snapshot(aid)

	timeout := int(b.repo.commitTimeouts.Load())

	require.Positive(t, timeout, "no timeout was injected, the scenario proves nothing")
	require.Equal(t, timeout, len(s.ledger)-countErrs(result, nil), "every commit-timeout user must be in the ledger despite getting an error")
	b.assertSafety(aid, stock, s, result)
	b.assertSlots(aid, stock, 0)
}

func TestConsistency_RedisLoss(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 100, 50
	b := newBench(t, aid, stock, nil)

	result := b.buyAll(userIDs(buyers), aid)
	for _, r := range result {
		assert.NoError(t, r.err)
	}
	n, err := b.rdb.Del(b.ctx, rediskey.Users(aid)).Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "users key must exist and be deleted")
	result = b.buyAll(userIDs(buyers), aid)
	for _, r := range result {
		assert.ErrorIs(t, r.err, domain.ErrAlreadyBought)
	}
	s := b.snapshot(aid)
	b.assertSafety(aid, stock, s, result)
	assert.Len(t, s.ledger, buyers)
	b.assertSlots(aid, stock, 0)
}

func TestConsistency_SameUserAcrossActivities(t *testing.T) {
	t.Parallel()
	const aidA, aidB, stock, buyers = 1, 2, 10, 10
	b := newBench(t, aidA, stock, nil)
	b.addActivity(aidB, stock)

	for _, r := range b.buyAll(userIDs(buyers), aidA) {
		require.NoError(t, r.err)
	}
	for _, r := range b.buyAll(userIDs(buyers), aidB) {
		assert.NoError(t, r.err, "winning activity A must not block the same user in activity B")
	}

	for _, aid := range []int64{aidA, aidB} {
		s := b.snapshot(aid)
		assert.Len(t, s.ledger, buyers)
		b.assertSlots(aid, stock, 0)
	}
}

func TestConsistency_SameUserBurst(t *testing.T) {
	t.Parallel()
	const aid, stock, requests = 1, 10, 50
	b := newBench(t, aid, stock, nil)

	results := b.buyAll(repeatUser("u0", requests), aid)

	s := b.snapshot(aid)
	b.assertSafety(aid, stock, s, results)
	b.assertSlots(aid, stock, 0)
	assert.Len(t, s.ledger, 1)
	assert.Equal(t, 1, countErrs(results, nil), "exactly one request wins")
	assert.Equal(t, requests-1, countErrs(results, domain.ErrAlreadyAdmitted), "the rest are stopped by redis, never reaching db")
}

func TestConsistency_SameUserBurstRedisLoss(t *testing.T) {
	t.Parallel()
	const aid, stock, requests = 1, 10, 50
	b := newBench(t, aid, stock, nil)

	first := b.buyAll(repeatUser("u0", requests), aid)
	require.Equal(t, 1, countErrs(first, nil), "first burst must produce exactly one winner")

	n, err := b.rdb.Del(b.ctx, rediskey.Users(aid)).Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "users key must exist and be deleted")

	second := b.buyAll(repeatUser("u0", requests), aid)

	s := b.snapshot(aid)
	b.assertSafety(aid, stock, s, second)
	b.assertSlots(aid, stock, 0)
	assert.Len(t, s.ledger, 1, "redis forgot the winner, but db must still hold a single order")
	assert.Equal(t, 0, countErrs(second, nil), "nobody wins twice")
	assert.Equal(t, 1, countErrs(second, domain.ErrAlreadyBought), "one request slips past redis and is stopped by the db unique key")
	assert.Equal(t, requests-1, countErrs(second, domain.ErrAlreadyAdmitted), "the rest are stopped by redis again")
}

func TestConsistency_CrossesEndAt(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 10, 10
	b := newBench(t, aid, stock, nil)

	_, err := b.pool.Exec(b.ctx,
		`UPDATE seckill_activity SET end_at = now() - interval '1 minute' WHERE activity_id = $1`, aid)
	require.NoError(t, err)

	results := b.buyAll(userIDs(buyers), aid)

	s := b.snapshot(aid)
	b.assertSafety(aid, stock, s, results)
	b.assertSlots(aid, stock, 0)
	assert.Equal(t, buyers, countErrs(results, nil),
		"redis admitted inside the window, so db must honour the order even after end_at has passed")
	assert.Len(t, s.ledger, buyers)
}

func TestConsistency_MixedFaults(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 40, 60
	b := newBench(t, aid, stock, cycleFaults(faultNone, faultCrash, faultRedisTimeout, faultCommitTimeout, faultCommitThenCrash))

	results := b.buyAll(userIDs(buyers), aid)
	s := b.snapshot(aid)

	require.Positive(t, b.repo.crashes.Load(), "no crash was injected")
	require.Positive(t, b.repo.redisTimeouts.Load(), "no redis reply loss was injected")
	require.Positive(t, b.repo.commitTimeouts.Load(), "no commit timeout was injected")
	require.Positive(t, b.repo.commitCrashes.Load(), "no crash after commit was injected")
	b.assertSafety(aid, stock, s, results)
	held := b.slotSets(aid).active
	lost := 0
	for _, n := range b.repo.dropped {
		if !held[n] {
			lost++
		}
	}
	b.assertSlots(aid, stock, lost)
	assert.Equal(t, int(b.repo.commitTimeouts.Load()+b.repo.commitCrashes.Load()), len(s.ledger)-countErrs(results, nil),
		"ledger rows beyond reported successes must be exactly the users whose commit outcome was lost")
}

func TestConsistency_CrashAfterCommitLosesNothing(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 30, 40
	b := newBench(t, aid, stock, everyNth(3, faultCommitThenCrash))

	results := b.buyAll(userIDs(buyers), aid)
	s := b.snapshot(aid)

	crashes := int(b.repo.commitCrashes.Load())
	require.Positive(t, crashes, "no crash after commit was injected")
	require.Equal(t, crashes, len(s.ledger)-countErrs(results, nil), "every crashed buyer's order is in the ledger")
	b.assertSafety(aid, stock, s, results)
	b.assertSlots(aid, stock, 0)
	require.Empty(t, b.slotSets(aid).dup(), "a crashed process returns nothing to redis")

	require.NoError(t, repository.NewRebuilder(b.rdb, b.pool).Rebuild(b.ctx, aid))
	b.assertRebuilt(aid)
	retry := b.buyAll(userIDs(buyers), aid)
	assert.Equal(t, stock, countErrs(retry, domain.ErrAlreadyAdmitted), "rebuild re-admits every ledger owner, crashed or not")
	assert.Equal(t, buyers-stock, countErrs(retry, domain.ErrSoldOut))
}

func TestConsistency_RedisRestartReissuesHeldSlots(t *testing.T) {
	t.Parallel()
	const aid, stock = 1, 20
	const firstWave, secondWave = 30, 8
	b := newBench(t, aid, stock, nil)

	first := b.buyAll(userIDs(firstWave), aid)
	require.Equal(t, stock, countErrs(first, nil), "first wave must sell out exactly the stock")

	require.NoError(t, b.rdb.FlushDB(b.ctx).Err())
	b.seedRedis(aid, stock)

	secondUsers := userIDsFrom(100, secondWave)
	second := b.buyAll(secondUsers, aid)
	require.Equal(t, secondWave, countErrs(second, domain.ErrSlotTaken), "every re-issued slot is refused by the db")
	assert.EqualValues(t, secondWave, b.repo.dbRejects.Load())
	for _, u := range secondUsers {
		admitted, err := b.rdb.SIsMember(b.ctx, rediskey.Users(aid), u).Result()
		require.NoError(t, err)
		assert.False(t, admitted, "refused buyer %s must be un-admitted so they can retry", u)
	}

	s := b.snapshot(aid)
	b.assertSafety(aid, stock, s, append(first, second...))
	assert.Len(t, s.ledger, stock, "db must never sell more than its stock, whatever redis hands out")
	require.Len(t, b.slotSets(aid).dup(), stock-secondWave, "a refused slot must not go back to redis")
	b.assertSlots(aid, stock, 0)

	require.NoError(t, repository.NewRebuilder(b.rdb, b.pool).Rebuild(b.ctx, aid))
	b.assertRebuilt(aid)
}

func TestConsistency_ReserveDeadlineMidFlight(t *testing.T) {
	t.Parallel()
	const stock, buyers = 40, 60
	timeouts := []time.Duration{2, 10, 15, 20, 30, 50}
	b := newBench(t, 1, stock, nil)

	deadlines, unknown := 0, 0
	for i, d := range timeouts {
		aid := int64(100 + i)
		b.addActivity(aid, stock)
		b.svc.reserveTimeout = d * time.Millisecond

		results := b.buyAll(userIDs(buyers), aid)
		s := b.snapshot(aid)
		deadlines += countErrs(results, context.DeadlineExceeded)
		unknown += len(s.ledger) - countErrs(results, nil)
		t.Logf("timeout=%v won=%d deadline=%d committed despite error=%d",
			b.svc.reserveTimeout, countErrs(results, nil), countErrs(results, context.DeadlineExceeded), len(s.ledger)-countErrs(results, nil))

		b.assertSafety(aid, stock, s, results)
		b.assertSlots(aid, stock, 0)
	}
	t.Logf("commits with unknown outcome across the sweep: %d", unknown)
	require.Positive(t, deadlines, "no reserve hit its deadline, the scenario proves nothing")
}
