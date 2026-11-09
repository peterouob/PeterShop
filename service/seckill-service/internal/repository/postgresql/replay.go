package postgresql

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/pkg/mq"
	seckill "github.com/peterouob/seckill_service/service/seckill-service/internal/repository/postgresql/db"
)

func ReplayOrders(ctx context.Context, pool *pgxpool.Pool, p mq.Producer, activityID int64, batchSize int32) (sent int, err error) {
	if batchSize <= 0 {
		return 0, fmt.Errorf("replay orders: batch size must be positive %d", batchSize)
	}

	q := seckill.New(pool)
	var afterOrderNo int64

	for {
		items, err := q.ListReplayEvents(ctx, seckill.ListReplayEventsParams{
			ActivityID:   activityID,
			AfterOrderNo: afterOrderNo,
			BatchSize:    batchSize,
		})

		if err != nil {
			return sent, fmt.Errorf("replay orders: %w after order: %d", err, afterOrderNo)
		}

		if len(items) == 0 {
			return sent, nil
		}

		msgs := make([]mq.Message, 0, len(items))
		for _, item := range items {
			payload, err := event.Encode(event.OrderReserved(item))
			if err != nil {
				return sent, fmt.Errorf("replay orders: %w after order: %d", err, afterOrderNo)
			}

			msgs = append(msgs, mq.Message{
				Key:     strconv.FormatInt(item.OrderNo, 10),
				Payload: payload},
			)
		}

		if err := p.SendBatch(ctx, event.TopicOrderReserved, msgs); err != nil {
			return sent, fmt.Errorf("replay orders: %w after order: %d", err, afterOrderNo)
		}

		sent += len(msgs)
		afterOrderNo = items[len(items)-1].OrderNo
	}
}
