package consumer

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/pkg/mq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOrders struct {
	got []int64
	err error
}

func (f *fakeOrders) CreateOrders(_ context.Context, evs []event.OrderReserved) error {
	for _, e := range evs {
		f.got = append(f.got, e.OrderNo)
	}
	return f.err
}

type fakeDLQ struct {
	topics []string
	sent   []mq.Message
	err    error
}

func (f *fakeDLQ) Send(ctx context.Context, topic string, msg mq.Message) error {
	return f.SendBatch(ctx, topic, []mq.Message{msg})
}

func (f *fakeDLQ) SendBatch(_ context.Context, topic string, msgs []mq.Message) error {
	if f.err != nil {
		return f.err
	}
	for range msgs {
		f.topics = append(f.topics, topic)
	}
	f.sent = append(f.sent, msgs...)
	return nil
}

func (f *fakeDLQ) SendDelay(context.Context, string, mq.Message, time.Duration) error {
	return mq.ErrDelayUnsupported
}

func (f *fakeDLQ) Close() error { return nil }

func good(t *testing.T, orderNo int64) mq.Message {
	t.Helper()
	b, err := event.Encode(event.OrderReserved{
		OrderNo: orderNo, UserID: "u" + strconv.FormatInt(orderNo, 10), ProductID: "p1",
		ActivityID: 1, Amount: 100, CreatedAtMs: time.Now().UnixMilli(),
	})
	require.NoError(t, err)
	return mq.Message{Key: strconv.FormatInt(orderNo, 10), Payload: b}
}

func poison(key string) mq.Message {
	return mq.Message{Key: key, Payload: []byte(`{"order_no":`)}
}

func newHandler() (*Handler, *fakeOrders, *fakeDLQ) {
	orders, dlq := &fakeOrders{}, &fakeDLQ{}
	return NewHandler(orders, dlq), orders, dlq
}

func TestHandle_AllGood(t *testing.T) {
	h, orders, dlq := newHandler()

	require.NoError(t, h.Handle(t.Context(), []mq.Message{good(t, 1), good(t, 2)}))

	assert.Equal(t, []int64{1, 2}, orders.got)
	assert.Empty(t, dlq.sent)
}

func TestHandle_PoisonToDLQ(t *testing.T) {
	h, orders, dlq := newHandler()
	bad := poison("bad-1")

	require.NoError(t, h.Handle(t.Context(), []mq.Message{good(t, 1), bad, good(t, 2)}))

	require.Len(t, dlq.sent, 1)
	assert.Equal(t, []string{event.TopicOrderReservedDLQ}, dlq.topics)
	assert.Equal(t, bad.Key, dlq.sent[0].Key)
	assert.Equal(t, bad.Payload, dlq.sent[0].Payload, "the DLQ keeps the original bytes for replay")
	assert.NotEmpty(t, dlq.sent[0].Headers[dlqErrorHeader], "the DLQ message says why it was rejected")
	assert.Equal(t, []int64{1, 2}, orders.got)
}

func TestHandle_AllPoisonCommits(t *testing.T) {
	h, orders, dlq := newHandler()

	require.NoError(t, h.Handle(t.Context(), []mq.Message{poison("a"), poison("b")}))

	assert.Len(t, dlq.sent, 2)
	assert.Empty(t, orders.got)
}

func TestHandle_DLQFailNoCommit(t *testing.T) {
	h, _, dlq := newHandler()
	dlq.err = errors.New("kafka down")

	err := h.Handle(t.Context(), []mq.Message{good(t, 1), poison("bad")})

	require.ErrorIs(t, err, dlq.err)
}

func TestHandle_ServiceFailNoCommit(t *testing.T) {
	h, orders, _ := newHandler()
	orders.err = errors.New("pg down")

	err := h.Handle(t.Context(), []mq.Message{good(t, 1)})

	require.ErrorIs(t, err, orders.err)
}
