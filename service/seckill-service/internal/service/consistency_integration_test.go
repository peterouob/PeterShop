//go:build integration

package service

import (
	"errors"
	"testing"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsistency_BenchSmoke(t *testing.T) { newBench(t, 1, 10, nil) }

func TestConsistency_CrashBeforeReserve(t *testing.T) {
	const aid, stock, buyers = 1, 30, 40
	b := newBench(t, aid, stock, everyNth(3, faultCrash))

	results := b.buyAll(userIDs(buyers), aid)
	s := b.snapshot(aid)

	require.Positive(t, b.repo.crashes.Load(), "no crash was injected, the scenario proves nothing")
	assertInvariants(t, stock, s, results)
	b.assertAccounting(stock, s)

	succeeded := 0
	for _, r := range results {
		if r.err == nil {
			succeeded++
		}
	}
	require.Equal(t, len(s.ledger), succeeded, "without commit timeouts, every ledger row must be a reported success")
}

func TestConsistency_RedisTimeoutThenRetry(t *testing.T) {
	const aid, stock, buyers = 1, 30, 40
	b := newBench(t, aid, stock, everyNth(3, faultRedisTimeout))

	result := b.buyAll(userIDs(buyers), aid)
	s := b.snapshot(aid)

	timeout := int(b.repo.redisTimeouts.Load())
	require.Positive(t, timeout, "no timeout was injected, the scenario proves nothing")
	assertInvariants(t, stock, s, result)
	b.assertAccounting(stock, s)

	result = b.buyAll(userIDs(buyers), aid)
	errCount := 0
	for _, r := range result {
		if errors.Is(r.err, domain.ErrAlreadyAdmitted) {
			errCount++
		}
	}
	assert.Equal(t, 30, errCount)
}

func TestConsistency_CommitTimeout(t *testing.T) {
	const aid, stock, buyers = 1, 30, 55
	b := newBench(t, aid, stock, everyNth(3, faultCommitTimeout))

	result := b.buyAll(userIDs(buyers), aid)
	s := b.snapshot(aid)

	timeout := int(b.repo.commitTimeouts.Load())

	var success int
	for _, r := range result {
		if r.err == nil {
			success++
		}
	}

	require.Equal(t, timeout, len(s.ledger)-success, "every commit-timeout user must be in the ledger despite getting an error")
	assertInvariants(t, stock, s, result)
	b.assertAccounting(stock, s)
}

func TestConsistency_RedisLoss(t *testing.T) {
	const aid, stock, buyers = 1, 100, 50
	b := newBench(t, aid, stock, nil)

	result := b.buyAll(userIDs(buyers), aid)
	for _, r := range result {
		assert.NoError(t, r.err)
	}
	b.rdb.Del(b.ctx, rediskey.Users(aid))
	result = b.buyAll(userIDs(buyers), aid)
	for _, r := range result {
		assert.ErrorIs(t, r.err, domain.ErrAlreadyBought)
	}
	s := b.snapshot(aid)
	b.assertAccounting(stock, s)
	assertInvariants(t, stock, s, result)
	assert.Equal(t, 50, len(s.ledger))
}

func TestConsistency_SameUserAcrossActivities(t *testing.T) {
	const aidA, aidB, stock, buyers = 1, 2, 10, 10
	b := newBench(t, aidA, stock, nil)
	b.addActivity(aidB, stock)

	for _, r := range b.buyAll(userIDs(buyers), aidA) {
		require.NoError(t, r.err)
	}
	for _, r := range b.buyAll(userIDs(buyers), aidB) {
		assert.NoError(t, r.err, "winning activity A must not block the same user in activity B")
	}

	assert.Len(t, b.snapshot(aidA).ledger, buyers)
	assert.Len(t, b.snapshot(aidB).ledger, buyers)
}

func TestConsistency_SameUserBurst(t *testing.T) {
	const aid, stock, buyers = 1, 10, 1
	b := newBench(t, aid, stock, nil)

	users := make([]string, 50)
	for i := range users {
		users[i] = "u0"
	}

	results := b.buyAll(users, aid)

	errCount := 0
	for _, result := range results {
		if errors.Is(result.err, domain.ErrAlreadyAdmitted) {
			errCount++
		}
	}

	s := b.snapshot(aid)
	assertInvariants(t, stock, s, results)
	assert.Equal(t, 1, len(s.ledger))
	assert.Equal(t, 49, errCount)
}

func TestConsistency_SameUserBurstRedisLoss(t *testing.T) {}
func TestConsistency_RetryAfterDBFailure(t *testing.T)    {}
func TestConsistency_CrossesEndAt(t *testing.T)           {}
