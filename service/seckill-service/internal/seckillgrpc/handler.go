package seckillgrpc

import (
	"context"
	"errors"
	"strconv"

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

func (h *Handler) Seckill(ctx context.Context, in *seckillproto.SeckillRequest) (*seckillproto.SeckillResponse, error) {
	if in.GetUserId() == "" || in.GetProductId() == "" {
		return nil, status.Error(codes.InvalidArgument, "userId and productId are required")
	}

	activityID, err := strconv.ParseInt(in.GetProductId(), 10, 64)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid productId")
	}

	if activityID <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid productId")
	}

	if err := h.seckill.Buy(ctx, activityID, in.GetUserId()); err != nil {
		return nil, toStatus(err)
	}

	return &seckillproto.SeckillResponse{
		BuyState: seckillproto.BuyState_SUCCESS,
		Msg:      "purchase accepted",
	}, nil
}

func toStatus(err error) error {
	switch {
	case errors.Is(err, domain.ErrAlreadyBought):
		return status.Error(codes.AlreadyExists, "you have already bought this product")
	case errors.Is(err, domain.ErrSoldOut):
		return status.Error(codes.ResourceExhausted, "product is sold out")
	case errors.Is(err, domain.ErrNotOpen):
		return status.Error(codes.FailedPrecondition, "the activity has not started")
	default:
		logger.Errorf(err, "seckill: unhandled failure")
		return status.Error(codes.Internal, "seckill request failed")
	}
}
