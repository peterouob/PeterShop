package seckillgrpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/peterouob/seckill_service/api/seckillproto"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeSeckill struct {
	called     bool
	userID     string
	activityID int64
	orderNo    int64
}

func (f *fakeSeckill) Buy(_ context.Context, activityID int64, userID string) (int64, error) {
	f.called, f.userID, f.activityID = true, userID, activityID
	return f.orderNo, nil
}

func TestBuy_PassesRequestAndReturnsOrderNo(t *testing.T) {
	fake := &fakeSeckill{orderNo: 1852301234567890123}
	resp, err := NewHandler(fake).Buy(context.Background(),
		&seckillproto.BuyRequest{UserId: "u1", ActivityId: 42})

	require.NoError(t, err)
	assert.Equal(t, "u1", fake.userID)
	assert.Equal(t, int64(42), fake.activityID)
	assert.Equal(t, seckillproto.BuyState_BUY_STATE_SUCCESS, resp.GetBuyState())
	assert.Equal(t, int64(1852301234567890123), resp.GetOrderNo())
}

func TestBuy_RejectsInvalidRequest(t *testing.T) {
	cases := []struct {
		name string
		in   *seckillproto.BuyRequest
	}{
		{"missing user", &seckillproto.BuyRequest{ActivityId: 1}},
		{"missing activity", &seckillproto.BuyRequest{UserId: "u1"}},
		{"negative activity", &seckillproto.BuyRequest{UserId: "u1", ActivityId: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSeckill{}
			_, err := NewHandler(fake).Buy(context.Background(), tc.in)

			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.False(t, fake.called, "invalid input must not reach Buy")
		})
	}
}

func TestToStatus(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{"already bought", domain.ErrAlreadyBought, codes.AlreadyExists},
		{"already admitted", domain.ErrAlreadyAdmitted, codes.AlreadyExists},
		{"sold out", domain.ErrSoldOut, codes.ResourceExhausted},
		{"not preheated", domain.ErrNotPreheated, codes.FailedPrecondition},
		{"not open", domain.ErrNotOpen, codes.FailedPrecondition},
		{"sealed", domain.ErrSealed, codes.FailedPrecondition},
		{"slot taken", domain.ErrSlotTaken, codes.Aborted},
		{"slot disabled", domain.ErrSlotDisabled, codes.Aborted},
		{"slot not found", domain.ErrSlotNotFound, codes.Aborted},
		{"stock lost", redis.ErrStockLost, codes.Internal},
		{"wrapped sealed", fmt.Errorf("buy: %w", domain.ErrSealed), codes.FailedPrecondition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantCode, status.Code(toStatus(tc.err)))
		})
	}
}

func TestToStatus_SameCodeHasDistinctMessages(t *testing.T) {
	groups := map[codes.Code][]error{
		codes.AlreadyExists:      {domain.ErrAlreadyBought, domain.ErrAlreadyAdmitted},
		codes.FailedPrecondition: {domain.ErrNotPreheated, domain.ErrNotOpen, domain.ErrSealed},
	}
	for code, errs := range groups {
		seen := map[string]error{}
		for _, err := range errs {
			msg := status.Convert(toStatus(err)).Message()
			if prev, dup := seen[msg]; dup {
				t.Errorf("%s: %v and %v share message %q", code, prev, err, msg)
			}
			seen[msg] = err
		}
	}
}
