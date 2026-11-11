package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate"
	"github.com/peterouob/seckill_service/pkg/logger"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/redis"
)

const reserveTimeout = 3 * time.Second

type SeckillService interface {
	Buy(ctx context.Context, activityID int64, userID string) (int64, error)
}

type seckillService struct {
	repo   repository.SeckillRepo
	nextID func() (int64, error)
}

func NewSeckillService(repo repository.SeckillRepo, sf *idgenerate.Snowflake) (SeckillService, error) {
	return &seckillService{repo: repo, nextID: sf.Next}, nil
}

func (s *seckillService) Buy(ctx context.Context, activityID int64, userID string) (int64, error) {
	orderID, err := s.nextID()
	if err != nil {
		return 0, fmt.Errorf("error for next id: %w", err)
	}

	slotNo, err := s.repo.DeductStock(ctx, activityID, userID)
	if err != nil {
		return 0, fmt.Errorf("seckill buy activity=%d user=%s: %w", activityID, userID, err)
	}

	rCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reserveTimeout)
	defer cancel()

	if err := s.repo.Reserve(rCtx, orderID, activityID, userID, slotNo); err != nil {
		cCtx, cancel := context.WithTimeout(context.WithoutCancel(rCtx), 10*time.Second)
		defer cancel()
		s.compensationFor(cCtx, activityID, userID, slotNo, err)
		return 0, fmt.Errorf("seckill reserve order_id=%d user=%s: %w", orderID, userID, err)
	}

	return orderID, nil
}

func (s *seckillService) compensationFor(ctx context.Context, activityID int64, userID string, slotNo int32, err error) {
	returnSlot, removeUser := compensationPlan(err)
	if !returnSlot && !removeUser {
		return
	}

	if errors.Is(err, domain.ErrSlotNotFound) {
		logger.Errorf(err, "seckill compensation slot_no=%d activity=%d user=%s ", slotNo, activityID, userID)
	}

	var slotArg int32
	if returnSlot {
		slotArg = slotNo
	}

	var lastErr error
retry:
	for attempt := range 3 {
		err := s.repo.Release(ctx, activityID, slotArg, userID, removeUser)
		if err == nil {
			return
		}
		if errors.Is(err, redis.ErrStockLost) {
			logger.Warn(fmt.Sprintf("release skipped: stock state lost, activity=%d user=%s", activityID, userID))
			return
		}
		lastErr = err
		select {
		case <-ctx.Done():
			break retry
		case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
		}
	}

	logger.Errorf(lastErr, "seckill compensation failed slot_no=%d activity=%d user=%s waiting for rebuild", slotArg, activityID, userID)
}

func compensationPlan(err error) (returnSlot, removeUser bool) {
	switch {
	case err == nil:
		return false, false
	case errors.Is(err, domain.ErrAlreadyBought):
		return true, false
	case errors.Is(err, domain.ErrSlotTaken),
		errors.Is(err, domain.ErrSlotDisabled),
		errors.Is(err, domain.ErrSlotNotFound):
		return false, true
	default:
		return true, true
	}
}
