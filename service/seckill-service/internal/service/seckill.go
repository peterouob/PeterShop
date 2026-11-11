package service

import (
	"context"
	"fmt"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
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

	if err := s.repo.DeductStock(ctx, activityID, userID); err != nil {
		return 0, fmt.Errorf("seckill buy activity=%d user=%s: %w", activityID, userID, err)
	}

	rCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reserveTimeout)
	defer cancel()

	if err := s.repo.Reserve(rCtx, orderID, activityID, userID, 0); err != nil {
		return 0, fmt.Errorf("seckill reserve order_id=%d user=%s: %w", orderID, userID, err)
	}

	return orderID, nil
}
