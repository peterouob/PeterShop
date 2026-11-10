package consumer

import (
	"context"

	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/pkg/mq"
	"github.com/peterouob/seckill_service/service/order-service/internal/service"
)

const dlqErrorHeader = "error"

type Handler struct {
	orders service.OrderService
	dlq    mq.Producer
}

func NewHandler(orders service.OrderService, dlq mq.Producer) *Handler {
	return &Handler{orders: orders, dlq: dlq}
}

func (h *Handler) Handle(ctx context.Context, msgs []mq.Message) error {
	msg, dlq := event.DecodeBatch(msgs)

	// make sure here is idempotent
	if err := h.orders.CreateOrders(ctx, msg); err != nil {
		return err
	}

	if len(dlq) == 0 {
		return nil
	}

	dlqMsg := make([]mq.Message, 0, len(dlq))

	for _, poison := range dlq {
		dlqMsg = append(dlqMsg, mq.WithErrorHeader(poison.Msg, dlqErrorHeader, poison.Err))
	}

	if err := h.dlq.SendBatch(ctx, event.TopicOrderReservedDLQ, dlqMsg); err != nil {
		return err
	}

	return nil
}
