package kafka

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/mq"
)

type producer struct {
	sync   sarama.SyncProducer
	closed atomic.Bool
}

func NewProducer(cfg *config.Config) (mq.Producer, error) {
	sc, err := producerConfig(cfg.Kafka)
	if err != nil {
		return nil, err
	}

	sp, err := sarama.NewSyncProducer(cfg.Kafka.Brokers, sc)
	if err != nil {
		return nil, fmt.Errorf("kafka: create sync producer for %v: %w", cfg.Kafka.Brokers, err)
	}
	return &producer{sync: sp}, nil
}

func producerConfig(k config.Kafka) (*sarama.Config, error) {
	acks := sarama.RequiredAcks(k.RequiredAcks)
	if acks != sarama.WaitForAll {
		return nil, fmt.Errorf("kafka: KAFKA_REQUIRED_ACKS must be -1 (all) to guarantee durability, got %d", k.RequiredAcks)
	}

	c := sarama.NewConfig()
	c.ClientID = k.ClientID
	c.Producer.RequiredAcks = acks
	c.Producer.Idempotent = true
	c.Producer.Return.Successes = true
	c.Producer.Return.Errors = true
	c.Producer.Retry.Max = k.MaxRetries
	c.Producer.Retry.Backoff = k.RetryBackoff
	c.Producer.Compression = sarama.CompressionZSTD
	c.Producer.Partitioner = sarama.NewHashPartitioner
	c.Net.MaxOpenRequests = 1
	return c, nil
}

func (p *producer) Send(ctx context.Context, topic string, msg mq.Message) error {
	return p.SendBatch(ctx, topic, []mq.Message{msg})
}

func (p *producer) SendBatch(ctx context.Context, topic string, msgs []mq.Message) error {
	if p.closed.Load() {
		return mq.ErrProducerClosed
	}
	if len(msgs) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	records := make([]*sarama.ProducerMessage, len(msgs))
	for i, m := range msgs {
		records[i] = toRecord(topic, m)
	}

	if err := p.sync.SendMessages(records); err != nil {
		return fmt.Errorf("kafka: publish %d message(s) to %s: %w", len(records), topic, err)
	}
	return nil
}

func (p *producer) SendDelay(context.Context, string, mq.Message, time.Duration) error {
	return mq.ErrDelayUnsupported
}

func (p *producer) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	if err := p.sync.Close(); err != nil {
		return fmt.Errorf("kafka: close producer: %w", err)
	}
	return nil
}

func toRecord(topic string, m mq.Message) *sarama.ProducerMessage {
	record := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(m.Payload),
	}
	if m.Key != "" {
		record.Key = sarama.StringEncoder(m.Key)
	}
	for k, v := range m.Headers {
		record.Headers = append(record.Headers, sarama.RecordHeader{
			Key:   []byte(k),
			Value: []byte(v),
		})
	}
	return record
}
