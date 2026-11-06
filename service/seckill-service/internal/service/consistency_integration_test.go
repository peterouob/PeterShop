//go:build integration

package service

import (
	"testing"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
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

	require.Positive(t, b.repo.crashes.Load(), "no crash was injected, the scenario proves nothing")
	assertInvariants(t, stock, s, results)
	assertDBLedger(t, s)
	b.assertAccounting(stock, s)

	require.Equal(t, len(s.ledger), countErrs(results, nil), "without commit timeouts, every ledger row must be a reported success")

	retry := b.buyAll(userIDs(buyers), aid)
	assert.Equal(t, stock, countErrs(retry, domain.ErrAlreadyAdmitted),
		"everyone redis admitted, crashed or not, is told 'already accepted', never 'already bought'")
}

func TestConsistency_RedisTimeoutThenRetry(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 30, 40
	b := newBench(t, aid, stock, everyNth(3, faultRedisTimeout))

	result := b.buyAll(userIDs(buyers), aid)
	s := b.snapshot(aid)

	timeout := int(b.repo.redisTimeouts.Load())
	require.Positive(t, timeout, "no timeout was injected, the scenario proves nothing")
	assertInvariants(t, stock, s, result)
	assertDBLedger(t, s)
	b.assertAccounting(stock, s)

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
	assertInvariants(t, stock, s, result)
	assertDBLedger(t, s)
	b.assertAccounting(stock, s)
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
	b.assertAccounting(stock, s)
	assertInvariants(t, stock, s, result)
	assertDBLedger(t, s)
	assert.Len(t, s.ledger, buyers)
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
		assertDBLedger(t, s)
		assert.Len(t, s.ledger, buyers)
	}
}

func TestConsistency_SameUserBurst(t *testing.T) {
	t.Parallel()
	const aid, stock, requests = 1, 10, 50
	b := newBench(t, aid, stock, nil)

	results := b.buyAll(repeatUser("u0", requests), aid)

	s := b.snapshot(aid)
	assertInvariants(t, stock, s, results)
	assertDBLedger(t, s)
	b.assertAccounting(stock, s)
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
	assertInvariants(t, stock, s, second)
	assertDBLedger(t, s)
	b.assertAccounting(stock, s)
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
	assertInvariants(t, stock, s, results)
	assertDBLedger(t, s)
	b.assertAccounting(stock, s)
	assert.Equal(t, buyers, countErrs(results, nil),
		"redis admitted inside the window, so db must honour the order even after end_at has passed")
	assert.Len(t, s.ledger, buyers)
}

func TestConsistency_MixedFaults(t *testing.T) {
	t.Parallel()
	const aid, stock, buyers = 1, 40, 60
	b := newBench(t, aid, stock, cycleFaults(faultNone, faultCrash, faultRedisTimeout, faultCommitTimeout))

	results := b.buyAll(userIDs(buyers), aid)
	s := b.snapshot(aid)

	require.Positive(t, b.repo.crashes.Load(), "no crash was injected")
	require.Positive(t, b.repo.redisTimeouts.Load(), "no redis reply loss was injected")
	require.Positive(t, b.repo.commitTimeouts.Load(), "no commit timeout was injected")
	assertInvariants(t, stock, s, results)
	assertDBLedger(t, s)
	b.assertAccounting(stock, s)
	assert.Equal(t, int(b.repo.commitTimeouts.Load()), len(s.ledger)-countErrs(results, nil),
		"ledger rows beyond reported successes must be exactly the commit-timeout users")
}

func TestConsistency_RedisRestartResetsStock(t *testing.T) {
	t.Parallel()
	const aid, stock = 1, 20
	const firstWave, secondWave = 30, 20
	b := newBench(t, aid, stock, nil)

	first := b.buyAll(userIDs(firstWave), aid)
	require.Equal(t, stock, countErrs(first, nil), "first wave must sell out exactly the stock")

	require.NoError(t, b.rdb.FlushDB(b.ctx).Err())
	b.seedRedis(aid, stock)

	second := b.buyAll(userIDsFrom(100, secondWave), aid)

	s := b.snapshot(aid)
	assertDBLedger(t, s)
	b.assertAccounting(2*stock, s)
	assert.Len(t, s.ledger, stock, "db must never sell more than its stock, whatever redis hands out")
	assert.Zero(t, s.dbAvailable)
	assert.Equal(t, secondWave, countErrs(second, domain.ErrSoldOut), "every token from the re-preheat is refused")
	assert.EqualValues(t, secondWave, b.repo.dbRejects.Load(), "the refusals come from the db stock check, not from redis")
}
