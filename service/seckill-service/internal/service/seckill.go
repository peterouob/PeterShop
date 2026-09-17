package service

import (
	"context"
	"fmt"

	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
	"go.uber.org/fx"
)

type deductResult int

const (
	deductSoldOut    deductResult = 0
	deductSucceeded  deductResult = 1
	deductDuplicated deductResult = 2
	deductNotStarted deductResult = -1
)

type SeckillService interface {
	Buy(ctx context.Context, userID, productID string) error
}

type seckillService struct {
	repo    repository.SeckillRepo
	session *concurrency.Session
}

func NewSeckillService(lc fx.Lifecycle, cfg *config.Config, repo repository.SeckillRepo, client *clientv3.Client) (SeckillService, error) {
	session, err := concurrency.NewSession(client, concurrency.WithTTL(int(cfg.Etcd.LeaseTTL.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("seckill: create etcd session: %w", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(context.Context) error { return session.Close() },
	})

	return &seckillService{repo: repo, session: session}, nil
}

func (s *seckillService) Buy(ctx context.Context, userID, productID string) error {
	result, err := s.repo.DeductStock(ctx, productID, userID)
	if err != nil {
		return fmt.Errorf("seckill: deduct stock for user %s on product %s: %w", userID, productID, err)
	}

	switch deductResult(result) {
	case deductSucceeded:
		return s.reduceStock(ctx, productID)
	case deductDuplicated:
		return ErrAlreadyBought
	case deductSoldOut:
		return ErrSoldOut
	case deductNotStarted:
		return ErrNotStarted
	default:
		return fmt.Errorf("seckill: unexpected deduct result %d for product %s", result, productID)
	}
}

func (s *seckillService) reduceStock(ctx context.Context, productID string) error {
	mutex := concurrency.NewMutex(s.session, fmt.Sprintf("/seckill/locks/%s", productID))

	if err := mutex.Lock(ctx); err != nil {
		return fmt.Errorf("seckill: acquire lock for product %s: %w", productID, err)
	}
	defer func() {
		_ = mutex.Unlock(context.WithoutCancel(ctx))
	}()

	if err := s.repo.ReduceStock(ctx, productID); err != nil {
		return fmt.Errorf("seckill: reduce stock for product %s: %w", productID, err)
	}
	return nil
}
