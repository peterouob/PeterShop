//go:build integration && cdc

package repository

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/pkg/mq"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Smoke check for the bench itself: if this is red, the problem is wiring, not your assertions.
func TestCDCBench_Starts(t *testing.T) {
	b := newCDCBench(t)
	b.seedActivity(1, "p1", 100, 10)
}

func TestCDC_ReserveReachesKafka(t *testing.T) {
	b := newCDCBench(t)
	b.seedActivity(1, "p1", 100, 10)
	orderNo := int64(1)
	before := time.Now().UnixMilli()
	err := b.repo.Reserve(t.Context(), orderNo, 1, "uid1")
	require.NoError(t, err)
	msgs := b.consume(1, 60*time.Second)
	require.Len(t, msgs, 1)
	assert.Equal(t, strconv.FormatInt(orderNo, 10), msgs[0].Key)
	assertOrderEvent(t, msgs[0], orderNo, "uid1", before)
}

func TestCDC_ResumesFromOffsetAfterDowntime(t *testing.T) {
	b := newCDCBench(t)
	b.seedActivity(1, "p1", 100, 10)

	before := time.Now().UnixMilli()
	const orderA = int64(1)
	keyA := strconv.FormatInt(orderA, 10)
	err := b.repo.Reserve(t.Context(), orderA, 1, fmt.Sprintf("uid%d", orderA))
	require.NoError(t, err)
	first := b.consume(1, 60*time.Second)
	require.Len(t, first, 1)
	require.Equal(t, keyA, first[0].Key)

	b.stopDebezium()

	for orderNo := int64(2); orderNo <= 4; orderNo++ {
		err := b.repo.Reserve(t.Context(), orderNo, 1, fmt.Sprintf("uid%d", orderNo))
		require.NoError(t, err)
	}
	b.restartDebezium()

	msgs := b.consume(4, 60*time.Second)
	counts := countByKey(msgs)

	require.Equal(t, 1, counts[keyA], "A should not be duplicated, indicating offset is not saved")

	for orderNo := int64(2); orderNo <= 4; orderNo++ {
		require.Contains(t, counts, strconv.FormatInt(orderNo, 10))
	}

	for _, msg := range msgs {
		orderNo, err := strconv.ParseInt(string(msg.Key), 10, 64)
		require.NoError(t, err)

		assertOrderEvent(t, msg, orderNo, fmt.Sprintf("uid%d", orderNo), before)
	}
}

func TestCDC_FailsLoudlyOnLostSlot(t *testing.T) {
	b := newCDCBench(t)
	b.seedActivity(1, "p1", 100, 10)

	reserve := func(orderNo int64) string {
		t.Helper()
		err := b.repo.Reserve(t.Context(), orderNo, 1, fmt.Sprintf("uid%d", orderNo))
		require.NoError(t, err)
		return strconv.FormatInt(orderNo, 10)
	}

	keyA := reserve(1)
	first := b.consume(1, 60*time.Second)
	require.Len(t, first, 1)
	require.Equal(t, keyA, first[0].Key)

	b.stopDebezium()
	keyB := reserve(2)
	b.invalidateSlot()

	require.Error(t, b.dbz.Start(b.ctx), "debezium must not become ready on a lost slot")
	blocked := countByKey(b.consume(2, 10*time.Second))
	require.NotContains(t, blocked, keyB, "B must not be delivered while the slot is lost")

}

func TestCDC_RolledBackReserveEmitsNothing(t *testing.T) {
	b := newCDCBench(t)
	b.seedActivity(1, "p1", 100, 1)

	require.NoError(t, b.repo.Reserve(t.Context(), 1, 1, "uid1"))
	require.ErrorIs(t, b.repo.Reserve(t.Context(), 2, 1, "uid2"), domain.ErrSoldOut)

	counts := countByKey(b.consume(2, 10*time.Second))
	require.Equal(t, 1, counts["1"], "the committed order must be delivered once")
	require.NotContains(t, counts, "2", "a rolled-back order must not produce an event")
}

// assertOrderEvent pins the wire contract order-service decodes.
func assertOrderEvent(t *testing.T, msg mq.Message, orderNo int64, userID string, beforeMs int64) {
	t.Helper()
	got, err := event.Decode(msg.Payload)
	require.NoError(t, err)
	assert.InDelta(t, beforeMs, got.CreatedAtMs, 120_000, "created_at_ms must be unix ms, close to now")
	assert.Equal(t, event.OrderReserved{
		OrderNo:     orderNo,
		UserID:      userID,
		ProductID:   "p1",
		ActivityID:  1,
		Amount:      100,
		CreatedAtMs: got.CreatedAtMs,
	}, got)
}

func countByKey(msgs []mq.Message) map[string]int {
	counts := make(map[string]int, len(msgs))
	for _, m := range msgs {
		counts[m.Key]++
	}
	return counts
}
