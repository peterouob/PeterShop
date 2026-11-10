package service

import (
	"context"
	"time"

	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/service/order-service/internal/repository"
)

type OrderService interface {
	CreateOrders(ctx context.Context, evs []event.OrderReserved) error
}

type orderService struct {
	repo       repository.OrderRepo
	payTimeout time.Duration
}

func NewOrderService(repo repository.OrderRepo, cfg *config.Config) OrderService {
	return &orderService{repo: repo, payTimeout: cfg.Order.PayTimeout}
}

func (s *orderService) CreateOrders(ctx context.Context, evs []event.OrderReserved) error {
	if err := s.repo.InsertPendingOrders(ctx, evs, s.payTimeout); err != nil {
		return err
	}

	if err := s.repo.ScheduleExpiry(ctx, evs, s.payTimeout); err != nil {
		return err
	}

	return nil
}
