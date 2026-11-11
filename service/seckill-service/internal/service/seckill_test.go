package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/postgresql"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/redis"
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
	nextSlot      int32
	reservedSlot  int32

	reserveCtxErr      error
	reserveHasDeadline bool

	releaseErr   error
	releaseCalls []releaseCall
}

type releaseCall struct {
	slot    int32
	user    string
	removed bool
	ctxErr  error
}

var _ repository.SeckillRepo = (*fakeRepo)(nil)

func (f *fakeRepo) DeductStock(ctx context.Context, _ int64, _ string) (int32, error) {
	f.deductCalled = true

	if f.honorCancel && ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if f.deductErr != nil {
		return 0, f.deductErr
	}
	f.nextSlot++
	return f.nextSlot, nil
}

func (f *fakeRepo) Reserve(ctx context.Context, orderNo, _ int64, _ string, slotNo int32) error {
	f.reserveCalled = true
	f.reservedNo = orderNo
	f.reservedSlot = slotNo
	f.reserveCtxErr = ctx.Err()
	_, f.reserveHasDeadline = ctx.Deadline()
	return f.reserveErr
}

func (f *fakeRepo) Release(ctx context.Context, _ int64, slotNo int32, userID string, removed bool) error {
	f.releaseCalls = append(f.releaseCalls, releaseCall{slot: slotNo, user: userID, removed: removed, ctxErr: ctx.Err()})
	return f.releaseErr
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
	assert.Equal(t, int32(1), f.reservedSlot, "the slot Redis handed out must reach Reserve")
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

var compensationCases = []struct {
	name                   string
	err                    error
	returnSlot, removeUser bool
}{
	{"success", nil, false, false},
	{"already bought keeps admission", fmt.Errorf("reserve: %w", domain.ErrAlreadyBought), true, false},
	{"slot taken drops admission only", fmt.Errorf("reserve: %w", domain.ErrSlotTaken), false, true},
	{"slot disabled drops admission only", fmt.Errorf("reserve: %w", domain.ErrSlotDisabled), false, true},
	{"slot not found drops admission only", fmt.Errorf("reserve: %w", domain.ErrSlotNotFound), false, true},
	{"deadline exceeded undoes both", fmt.Errorf("reserve: %w", context.DeadlineExceeded), true, true},
	{"duplicate order no undoes both", fmt.Errorf("reserve: %w", postgresql.ErrDuplicateOrderNo), true, true},
}

func TestCompensationPlan(t *testing.T) {
	for _, tc := range compensationCases {
		t.Run(tc.name, func(t *testing.T) {
			returnSlot, removeUser := compensationPlan(tc.err)
			assert.Equal(t, tc.returnSlot, returnSlot, "returnSlot")
			assert.Equal(t, tc.removeUser, removeUser, "removeUser")
		})
	}
}

func buyWithReserveErr(t *testing.T, ctx context.Context, f *fakeRepo) error {
	t.Helper()
	svc := newTestService(f, func() (int64, error) { return 777, nil })
	_, err := svc.Buy(ctx, 1, "u1")
	return err
}

func TestBuy_CompensatesPerPlan(t *testing.T) {
	for _, tc := range compensationCases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRepo{reserveErr: tc.err}
			err := buyWithReserveErr(t, t.Context(), f)

			if tc.err == nil {
				assert.NoError(t, err)
				assert.Empty(t, f.releaseCalls, "success must not compensate")
				return
			}
			assert.ErrorIs(t, err, tc.err, "compensation must not replace the reserve error")
			require.Len(t, f.releaseCalls, 1)

			wantSlot := int32(0)
			if tc.returnSlot {
				wantSlot = f.reservedSlot
			}
			got := f.releaseCalls[0]
			assert.Equal(t, wantSlot, got.slot, "slot 0 means keep the slot out of the pool")
			assert.Equal(t, tc.removeUser, got.removed)
			assert.Equal(t, "u1", got.user)
		})
	}
}

func TestBuy_ReleaseStockLostIsNotRetried(t *testing.T) {
	reserveErr := errors.New("pg down")
	f := &fakeRepo{reserveErr: reserveErr, releaseErr: redis.ErrStockLost}

	err := buyWithReserveErr(t, t.Context(), f)
	assert.ErrorIs(t, err, reserveErr)
	assert.NotErrorIs(t, err, redis.ErrStockLost)
	assert.Len(t, f.releaseCalls, 1)
}

func TestBuy_ReleaseTransientErrorIsRetried(t *testing.T) {
	reserveErr := errors.New("pg down")
	f := &fakeRepo{reserveErr: reserveErr, releaseErr: errors.New("redis i/o timeout")}

	err := buyWithReserveErr(t, t.Context(), f)
	assert.ErrorIs(t, err, reserveErr)
	assert.Len(t, f.releaseCalls, 3)
}

func TestBuy_CompensationSurvivesClientCancel(t *testing.T) {
	f := &fakeRepo{reserveErr: errors.New("pg down")}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_ = buyWithReserveErr(t, ctx, f)
	require.Len(t, f.releaseCalls, 1, "a cancelled caller must not leak the slot")
	assert.NoError(t, f.releaseCalls[0].ctxErr)
}
