package seckillgrpc

import (
	"context"
	"errors"

	"github.com/peterouob/seckill_service/api/seckillproto"
	"github.com/peterouob/seckill_service/pkg/logger"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct {
	seckillproto.UnimplementedSeckillServiceServer
	seckill service.SeckillService
}

func NewHandler(seckill service.SeckillService) *Handler {
	return &Handler{seckill: seckill}
}

func (h *Handler) Buy(ctx context.Context, in *seckillproto.BuyRequest) (*seckillproto.BuyResponse, error) {
	if in.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if in.GetActivityId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "activity_id must be positive")
	}

	orderNo, err := h.seckill.Buy(ctx, in.GetActivityId(), in.GetUserId())
	if err != nil {
		return nil, toStatus(err)
	}

	return &seckillproto.BuyResponse{
		BuyState: seckillproto.BuyState_BUY_STATE_SUCCESS,
		Msg:      "purchase accepted",
		OrderNo:  orderNo,
	}, nil
}

func toStatus(err error) error {
	switch {
	case errors.Is(err, domain.ErrAlreadyBought):
		return status.Error(codes.AlreadyExists, "you have already bought this product")
	case errors.Is(err, domain.ErrAlreadyAdmitted):
		return status.Error(codes.AlreadyExists, "request already accepted, please check your orders")
	case errors.Is(err, domain.ErrSoldOut):
		return status.Error(codes.ResourceExhausted, "product is sold out")
	case errors.Is(err, domain.ErrNotOpen):
		return status.Error(codes.FailedPrecondition, "the activity has not started")
	case errors.Is(err, domain.ErrNotPreheated):
		return status.Error(codes.FailedPrecondition, "the activity has not been preheated")
	case errors.Is(err, domain.ErrSealed):
		return status.Error(codes.FailedPrecondition, "the activity has been sealed")
	case errors.Is(err, domain.ErrSlotTaken):
		return status.Error(codes.Aborted, "the slot has been taken please try again later")
	case errors.Is(err, domain.ErrSlotDisabled):
		return status.Error(codes.Aborted, "the slot has been disabled please try again later")
	case errors.Is(err, domain.ErrSlotNotFound):
		logger.Errorf(err, "seckill: slot not found and redis not the same of db")
		return status.Error(codes.Aborted, "the slot not found please try again later")
	default:
		logger.Errorf(err, "seckill: unhandled failure")
		return status.Error(codes.Internal, "seckill request failed")
	}
}
