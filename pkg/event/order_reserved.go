package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/peterouob/seckill_service/pkg/mq"
)

const TopicOrderReserved = "seckill-order"

var ErrPoison = errors.New("event: poison message")

type OrderReserved struct {
	OrderNo     int64  `json:"order_no"`
	UserID      string `json:"user_id"`
	ProductID   string `json:"product_id"`
	ActivityID  int64  `json:"activity_id"`
	Amount      int64  `json:"amount"`
	CreatedAtMs int64  `json:"created_at_ms"`
}

const minCreatedAtMs = 1_000_000_000_000

func (e OrderReserved) validate() error {
	if e.OrderNo <= 0 {
		return fmt.Errorf("%w: order_no must be > 0, got %d", ErrPoison, e.OrderNo)
	}
	if strings.TrimSpace(e.UserID) == "" {
		return fmt.Errorf("%w: user_id is empty", ErrPoison)
	}
	if strings.TrimSpace(e.ProductID) == "" {
		return fmt.Errorf("%w: product_id is empty", ErrPoison)
	}
	if e.ActivityID <= 0 {
		return fmt.Errorf("%w: activity_id must be > 0, got %d", ErrPoison, e.ActivityID)
	}
	if e.Amount <= 0 {
		return fmt.Errorf("%w: amount must be > 0, got %d", ErrPoison, e.Amount)
	}
	if e.CreatedAtMs < minCreatedAtMs {
		return fmt.Errorf("%w: created_at_ms must be >= %d (unix ms), got %d",
			ErrPoison, minCreatedAtMs, e.CreatedAtMs)
	}
	return nil
}

func (e OrderReserved) ExpireAtMs(payTimeout time.Duration) int64 {
	return e.CreatedAtMs + payTimeout.Milliseconds()
}

type Poison struct {
	Msg mq.Message
	Err error
}

func Decode(b []byte) (OrderReserved, error) {
	var orderPlaced OrderReserved
	if err := json.Unmarshal(b, &orderPlaced); err != nil {
		return OrderReserved{}, fmt.Errorf("%w error: %v", ErrPoison, err)
	}

	if err := orderPlaced.validate(); err != nil {
		return OrderReserved{}, err
	}

	return orderPlaced, nil
}

func Encode(o OrderReserved) ([]byte, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(o)
}

func DecodeBatch(msgs []mq.Message) (ok []OrderReserved, poison []Poison) {
	for _, msg := range msgs {
		orderPlaced, err := Decode(msg.Payload)
		if err != nil {
			poison = append(poison, Poison{Msg: msg, Err: err})
			continue
		}
		ok = append(ok, orderPlaced)
	}
	return ok, poison
}
