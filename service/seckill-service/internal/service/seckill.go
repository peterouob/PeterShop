package service

import (
	"context"
	"fmt"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
)

type deductResult int

const (
	deductSoldOut    deductResult = 0
	deductSucceeded  deductResult = 1
	deductDuplicated deductResult = 2
	deductNotStarted deductResult = -1
)

type SeckillService interface {
	Buy(ctx context.Context, activityID int64, userID string) error
}

type seckillService struct {
	repo repository.SeckillRepo
}

func NewSeckillService(repo repository.SeckillRepo) (SeckillService, error) {
	return &seckillService{repo: repo}, nil
}

func (s *seckillService) Buy(ctx context.Context, activityID int64, userID string) error {
	result, err := s.repo.DeductStock(ctx, activityID, userID)
	if err != nil {
		return fmt.Errorf("seckill: deduct stock for user %s on product %d: %w", userID, activityID, err)
	}

	switch deductResult(result) {
	case deductSucceeded:
		// TODO: reduce stock need to be moved to the order service
		return nil
	case deductDuplicated:
		return ErrAlreadyBought
	case deductSoldOut:
		return ErrSoldOut
	case deductNotStarted:
		return ErrNotStarted
	default:
		return fmt.Errorf("seckill: unexpected deduct result %d for product %d", result, activityID)
	}
}
