package service

import (
	"context"
	"fmt"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
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
	if err := s.repo.DeductStock(ctx, activityID, userID); err != nil {
		return fmt.Errorf("seckill buy activity=%d user=%s: %w", activityID, userID, err)
	}
	return nil
}
