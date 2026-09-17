package kafka

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/IBM/sarama"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/logger"
	"github.com/peterouob/seckill_service/pkg/mq"
)

const (
	backoffBase   = 200 * time.Millisecond
	backoffMax    = 30 * time.Second
	backoffJitter = 0.2
)

type consumer struct {
	group     sarama.ConsumerGroup
	groupID   string
	batchSize int
	flushWait time.Duration
}

func NewConsumer(cfg *config.Config, groupID string) (mq.Consumer, error) {
	if groupID == "" {
		return nil, errors.New("kafka: consumer group id must not be empty")
	}

	group, err := sarama.NewConsumerGroup(cfg.Kafka.Brokers, groupID, consumerConfig(cfg.Kafka))
	if err != nil {
		return nil, fmt.Errorf("kafka: create consumer group %q: %w", groupID, err)
	}

	return &consumer{
		group:     group,
		groupID:   groupID,
		batchSize: cfg.Kafka.BatchSize,
		flushWait: cfg.Kafka.FlushTimeout,
	}, nil
}

func consumerConfig(k config.Kafka) *sarama.Config {
	c := sarama.NewConfig()
	c.ClientID = k.ClientID
	c.Consumer.Return.Errors = true
	c.Consumer.Offsets.Initial = sarama.OffsetOldest
	c.Consumer.Offsets.AutoCommit.Enable = false
	c.Consumer.Retry.Backoff = k.RetryBackoff
	c.Consumer.MaxWaitTime = 500 * time.Millisecond
	c.Consumer.Fetch.Default = 1 << 20
	return c
}

func (c *consumer) Start(ctx context.Context, topics []string, handle mq.Handler) error {
	if len(topics) == 0 {
		return errors.New("kafka: at least one topic is required")
	}

	go c.drainErrors(ctx)

	h := &groupHandler{batchSize: c.batchSize, flushWait: c.flushWait, handle: handle}

	for attempt := 0; ; {
		err := c.group.Consume(ctx, topics, h)
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, sarama.ErrClosedConsumerGroup):
			return nil
		case err != nil:
			attempt++
			wait := backoff(attempt)
			logger.Errorf(err, "kafka: consume failed for group %s, retrying in %s", c.groupID, wait)
			if !sleep(ctx, wait) {
				return nil
			}
		default:
			attempt = 0
		}
	}
}

func (c *consumer) drainErrors(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-c.group.Errors():
			if !ok {
				return
			}
			logger.Errorf(err, "kafka: consumer group %s error", c.groupID)
		}
	}
}

func (c *consumer) Close() error {
	if err := c.group.Close(); err != nil {
		return fmt.Errorf("kafka: close consumer group %q: %w", c.groupID, err)
	}
	return nil
}

type groupHandler struct {
	batchSize int
	flushWait time.Duration
	handle    mq.Handler
}

func (h *groupHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *groupHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *groupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	batch := make([]*sarama.ConsumerMessage, 0, h.batchSize)
	ticker := time.NewTicker(h.flushWait)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-claim.Messages():
			if !ok {
				return h.flush(session, batch)
			}
			batch = append(batch, msg)
			if len(batch) < h.batchSize {
				continue
			}
			if err := h.flush(session, batch); err != nil {
				return err
			}
			batch = batch[:0]

		case <-ticker.C:
			if err := h.flush(session, batch); err != nil {
				return err
			}
			batch = batch[:0]

		case <-session.Context().Done():
			return h.flush(session, batch)
		}
	}
}

func (h *groupHandler) flush(session sarama.ConsumerGroupSession, batch []*sarama.ConsumerMessage) error {
	if len(batch) == 0 {
		return nil
	}

	msgs := make([]mq.Message, len(batch))
	for i, m := range batch {
		msgs[i] = fromRecord(m)
	}

	if err := h.handle(session.Context(), msgs); err != nil {
		return fmt.Errorf("kafka: handler rejected batch of %d, offsets not committed: %w", len(batch), err)
	}

	for _, m := range batch {
		session.MarkMessage(m, "")
	}
	session.Commit()
	return nil
}

func fromRecord(m *sarama.ConsumerMessage) mq.Message {
	msg := mq.Message{Key: string(m.Key), Payload: m.Value}
	if len(m.Headers) > 0 {
		msg.Headers = make(map[string]string, len(m.Headers))
		for _, hdr := range m.Headers {
			msg.Headers[string(hdr.Key)] = string(hdr.Value)
		}
	}
	return msg
}

func backoff(attempt int) time.Duration {
	d := float64(backoffBase) * math.Pow(2, float64(attempt-1))
	d = min(d, float64(backoffMax))
	d *= 1 + backoffJitter*(rand.Float64()*2-1)
	return time.Duration(d)
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
