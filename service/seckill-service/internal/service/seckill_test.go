package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/postgresql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepo struct {
	deductErr   error
	reserveErr  error
	honorCancel bool

	deductCalled  bool
	reserveCalled bool
	reservedNo    int64

	reserveCtxErr      error
	reserveHasDeadline bool
}

var _ repository.SeckillRepo = (*fakeRepo)(nil)

func (f *fakeRepo) DeductStock(ctx context.Context, _ int64, _ string) error {
	f.deductCalled = true

	if f.honorCancel && ctx.Err() != nil {
		return ctx.Err()
	}

	return f.deductErr
}

func (f *fakeRepo) Reserve(ctx context.Context, orderNo, _ int64, _ string, _ int32) error {
	f.reserveCalled = true
	f.reservedNo = orderNo
	f.reserveCtxErr = ctx.Err()
	_, f.reserveHasDeadline = ctx.Deadline()
	return f.reserveErr
}

func newTestService(repo *fakeRepo, nextID func() (int64, error)) *seckillService {
	return &seckillService{repo: repo, nextID: nextID}
}

func TestBuy_NextIDFails_TouchesNothing(t *testing.T) {
	f := &fakeRepo{}
	svc := newTestService(f, func() (int64, error) {
		return 0, errors.New("etcd lease lost")
	})

	_, err := svc.Buy(t.Context(), 1, "")
	assert.Error(t, err)
	assert.False(t, f.deductCalled)
	assert.False(t, f.reserveCalled)

}

func TestBuy_DeductFails_SkipsReserve(t *testing.T) {
	f := &fakeRepo{deductErr: domain.ErrSoldOut}
	svc := newTestService(f, func() (int64, error) {
		return 777, nil
	})

	_, err := svc.Buy(t.Context(), 1, "")
	assert.ErrorIs(t, err, domain.ErrSoldOut)
	assert.False(t, f.reserveCalled)
}

func TestBuy_Success_ReturnsReservedOrderNo(t *testing.T) {
	f := &fakeRepo{}
	svc := newTestService(f, func() (int64, error) {
		return 777, nil
	})

	id, err := svc.Buy(t.Context(), 1, "")
	assert.NoError(t, err)
	assert.Equal(t, int64(777), id)
	assert.Equal(t, int64(777), f.reservedNo)
}

func TestBuy_ReserveErrors(t *testing.T) {
	cases := []struct {
		name         string
		reserveError error
		wantBiz      error
	}{
		{"already bought", domain.ErrAlreadyBought, domain.ErrAlreadyBought},
		{"sold out", domain.ErrSoldOut, domain.ErrSoldOut},
		{"system error", fmt.Errorf("reserve: %w", postgresql.ErrDuplicateOrderNo), nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRepo{reserveErr: tc.reserveError}
			svc := newTestService(fake, func() (int64, error) {
				return 777, nil
			})
			id, err := svc.Buy(t.Context(), 1, "")
			assert.Error(t, err)
			assert.Zero(t, id)
			assert.Equal(t, int64(777), fake.reservedNo)

			if tc.wantBiz != nil {
				assert.ErrorIs(t, err, tc.wantBiz)
				return
			}

			_, isBiz := errors.AsType[*domain.Error](err)
			assert.False(t, isBiz)
		})
	}
}

func TestBuy_ReserveSurvivesClientCancel(t *testing.T) {
	f := &fakeRepo{}
	svc := newTestService(f, func() (int64, error) {
		return 777, nil
	})

	rCtx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _ = svc.Buy(rCtx, 1, "")
	require.True(t, f.reserveCalled)
	assert.NoError(t, f.reserveCtxErr)
	assert.True(t, f.reserveHasDeadline)
}

func TestBuy_CanceledBeforeDeduct_SkipsReserve(t *testing.T) {
	f := &fakeRepo{honorCancel: true}
	svc := newTestService(f, func() (int64, error) {
		return 777, nil
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := svc.Buy(ctx, 1, "")
	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, f.reserveCalled)
}
