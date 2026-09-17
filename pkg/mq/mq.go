package mq

import (
	"context"
	"errors"
	"time"
)

var (
	ErrDelayUnsupported = errors.New("mq: delayed delivery is not supported by this driver")
	ErrProducerClosed   = errors.New("mq: producer is closed")
)

type Message struct {
	Key     string
	Payload []byte
	Headers map[string]string
}

type Producer interface {
	Send(ctx context.Context, topic string, msg Message) error
	SendBatch(ctx context.Context, topic string, msgs []Message) error
	SendDelay(ctx context.Context, topic string, msg Message, delay time.Duration) error
	Close() error
}

type Handler func(ctx context.Context, msgs []Message) error

type Consumer interface {
	Start(ctx context.Context, topics []string, handle Handler) error
	Close() error
}
