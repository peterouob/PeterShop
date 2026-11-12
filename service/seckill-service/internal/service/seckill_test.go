package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/postgresql"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/redis"
	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testOpenTimeout = 20 * time.Millisecond

type fakeRepo struct {
	deductErr     error
	reserveErr    error
	reserveBlocks bool
	honorCancel   bool

	deducts, reserves, releases atomic.Int64

	reservedNo   int64
	nextSlot     int32
	reservedSlot int32

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
	f.deducts.Add(1)

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
	f.reserves.Add(1)
	f.reservedNo = orderNo
	f.reservedSlot = slotNo
	f.reserveCtxErr = ctx.Err()
	_, f.reserveHasDeadline = ctx.Deadline()
	if f.reserveBlocks {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.reserveErr
}

func (f *fakeRepo) Release(ctx context.Context, _ int64, slotNo int32, userID string, removed bool) error {
	f.releases.Add(1)
	f.releaseCalls = append(f.releaseCalls, releaseCall{slot: slotNo, user: userID, removed: removed, ctxErr: ctx.Err()})
	return f.releaseErr
}

func newTestService(repo *fakeRepo, nextID func() (int64, error)) *seckillService {
	return &seckillService{
		repo:           repo,
		nextID:         nextID,
		cb:             newReserveGate(testOpenTimeout),
		reserveTimeout: reserveTimeout,
	}
}

func TestBuy_NextIDFails_TouchesNothing(t *testing.T) {
	f := &fakeRepo{}
	svc := newTestService(f, func() (int64, error) {
		return 0, errors.New("etcd lease lost")
	})

	_, err := svc.Buy(t.Context(), 1, "")
	assert.Error(t, err)
	assert.Zero(t, f.deducts.Load())
	assert.Zero(t, f.reserves.Load())

}

func TestBuy_DeductFails_SkipsReserve(t *testing.T) {
	f := &fakeRepo{deductErr: domain.ErrSoldOut}
	svc := newTestService(f, func() (int64, error) {
		return 777, nil
	})

	_, err := svc.Buy(t.Context(), 1, "")
	assert.ErrorIs(t, err, domain.ErrSoldOut)
	assert.Zero(t, f.reserves.Load())
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
	require.EqualValues(t, 1, f.reserves.Load())
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
	assert.Zero(t, f.reserves.Load())
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

var errConn = errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")

type repoCounts struct{ deducts, reserves, releases int64 }

func countsOf(f *fakeRepo) repoCounts {
	return repoCounts{f.deducts.Load(), f.reserves.Load(), f.releases.Load()}
}

func newBreakerService(f *fakeRepo) *seckillService {
	return newTestService(f, func() (int64, error) { return 777, nil })
}

func buyN(t *testing.T, svc *seckillService, n int) {
	t.Helper()
	for range n {
		_, _ = svc.Buy(t.Context(), 1, "u1")
	}
}

func requireState(t *testing.T, svc *seckillService, want gobreaker.State) {
	t.Helper()
	require.Equal(t, want, svc.cb.Breaker().State())
}

func tripToHalfOpen(t *testing.T, svc *seckillService, f *fakeRepo) {
	t.Helper()
	f.reserveErr = errConn
	buyN(t, svc, tripAfterFailures)
	f.reserveErr = nil
	require.Eventually(t, func() bool {
		return svc.cb.Breaker().State() == gobreaker.StateHalfOpen
	}, time.Second, 2*time.Millisecond)
}

func TestBuy_Breaker_TripsOnConnectionErrors(t *testing.T) {
	f := &fakeRepo{reserveErr: errConn}
	svc := newBreakerService(f)
	svc.cb = newReserveGate(time.Minute)

	buyN(t, svc, tripAfterFailures)
	before := countsOf(f)
	require.Equal(t, repoCounts{tripAfterFailures, tripAfterFailures, tripAfterFailures}, before)

	_, err := svc.Buy(t.Context(), 1, "u1")
	assert.ErrorIs(t, err, domain.ErrUnavailable)
	assert.Equal(t, before, countsOf(f), "a rejected call must not touch redis or the db")
}

func TestBuy_Breaker_BusinessErrorsKeepClosed(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"already bought", domain.ErrAlreadyBought},
		{"slot taken", domain.ErrSlotTaken},
		{"slot not found", domain.ErrSlotNotFound},
		{"slot disabled", domain.ErrSlotDisabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRepo{reserveErr: tc.err}
			svc := newBreakerService(f)

			buyN(t, svc, 100)
			requireState(t, svc, gobreaker.StateClosed)

			before := f.deducts.Load()
			_, _ = svc.Buy(t.Context(), 1, "u1")
			assert.Equal(t, before+1, f.deducts.Load())
		})
	}
}

func TestBuy_Breaker_ReserveTimeoutTrips(t *testing.T) {
	f := &fakeRepo{reserveBlocks: true}
	svc := newBreakerService(f)
	svc.cb = newReserveGate(time.Minute)
	svc.reserveTimeout = 10 * time.Millisecond

	for range tripAfterFailures {
		_, err := svc.Buy(t.Context(), 1, "u1")
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
	requireState(t, svc, gobreaker.StateOpen)
}

func TestBuy_Breaker_HalfOpenProbeReturnedWhenDBNotReached(t *testing.T) {
	errRedis := errors.New("redis: i/o timeout")
	errID := errors.New("etcd lease lost")
	cases := []struct {
		name  string
		setup func(svc *seckillService, f *fakeRepo) (restore func())
		want  error
	}{
		{"sold out", func(_ *seckillService, f *fakeRepo) func() {
			f.deductErr = domain.ErrSoldOut
			return func() { f.deductErr = nil }
		}, domain.ErrSoldOut},
		{"redis error", func(_ *seckillService, f *fakeRepo) func() {
			f.deductErr = errRedis
			return func() { f.deductErr = nil }
		}, errRedis},
		{"next id fails", func(svc *seckillService, _ *fakeRepo) func() {
			next := svc.nextID
			svc.nextID = func() (int64, error) { return 0, errID }
			return func() { svc.nextID = next }
		}, errID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRepo{}
			svc := newBreakerService(f)
			tripToHalfOpen(t, svc, f)

			restore := tc.setup(svc, f)
			for range 3 * breakerProbes {
				_, err := svc.Buy(t.Context(), 1, "u1")
				require.ErrorIs(t, err, tc.want, "every call must be admitted as a probe")
			}
			restore()

			before := f.reserves.Load()
			_, err := svc.Buy(t.Context(), 1, "u1")
			require.NoError(t, err)
			assert.Equal(t, before+1, f.reserves.Load())
		})
	}
}

func TestBuy_Breaker_HalfOpenTransitions(t *testing.T) {
	t.Run("probes succeed closes", func(t *testing.T) {
		f := &fakeRepo{}
		svc := newBreakerService(f)
		tripToHalfOpen(t, svc, f)

		for range breakerProbes {
			_, err := svc.Buy(t.Context(), 1, "u1")
			require.NoError(t, err)
		}
		requireState(t, svc, gobreaker.StateClosed)
	})

	t.Run("probe fails reopens", func(t *testing.T) {
		f := &fakeRepo{}
		svc := newBreakerService(f)
		tripToHalfOpen(t, svc, f)

		f.reserveErr = errConn
		_, err := svc.Buy(t.Context(), 1, "u1")
		require.ErrorIs(t, err, errConn)
		requireState(t, svc, gobreaker.StateOpen)

		before := f.deducts.Load()
		_, err = svc.Buy(t.Context(), 1, "u1")
		assert.ErrorIs(t, err, domain.ErrUnavailable)
		assert.Equal(t, before, f.deducts.Load())
	})
}

func TestBuy_Breaker_BusinessErrorsDoNotDiluteFailures(t *testing.T) {
	f := &fakeRepo{reserveErr: domain.ErrAlreadyBought}
	svc := newBreakerService(f)
	svc.cb = newReserveGate(time.Minute)

	buyN(t, svc, 1000)
	f.reserveErr = errConn
	for i := 1; i <= tripAfterFailures; i++ {
		buyN(t, svc, 1)
		want := gobreaker.StateClosed
		if i == tripAfterFailures {
			want = gobreaker.StateOpen
		}
		requireState(t, svc, want)
	}
}

func TestBuy_Breaker_CountsConsecutiveFailures(t *testing.T) {
	f := &fakeRepo{}
	svc := newBreakerService(f)

	f.reserveErr = errConn
	buyN(t, svc, tripAfterFailures-1)
	f.reserveErr = nil
	buyN(t, svc, 1)
	f.reserveErr = errConn
	buyN(t, svc, tripAfterFailures-1)
	requireState(t, svc, gobreaker.StateClosed)

	before := f.deducts.Load()
	_, _ = svc.Buy(t.Context(), 1, "u1")
	assert.Equal(t, before+1, f.deducts.Load())
}

var breakerRejections = []struct {
	name string
	err  error
}{
	{"open state", gobreaker.ErrOpenState},
	{"too many requests", gobreaker.ErrTooManyRequests},
	{"wrapped open state", fmt.Errorf("reserve: %w", gobreaker.ErrOpenState)},
	{"wrapped too many requests", fmt.Errorf("reserve: %w", gobreaker.ErrTooManyRequests)},
}

func TestCompensationPlan_BreakerRejectionUndoesBoth(t *testing.T) {
	for _, tc := range breakerRejections {
		t.Run(tc.name, func(t *testing.T) {
			returnSlot, removeUser := compensationPlan(tc.err)
			assert.True(t, returnSlot, "returnSlot")
			assert.True(t, removeUser, "removeUser")
		})
	}
}

func TestReserveErr(t *testing.T) {
	t.Run("nil stays nil", func(t *testing.T) {
		assert.NoError(t, reserveErr(nil))
	})
	t.Run("other errors pass through", func(t *testing.T) {
		assert.Same(t, errConn, reserveErr(errConn))
	})
	for _, tc := range breakerRejections {
		t.Run(tc.name+" maps to unavailable", func(t *testing.T) {
			assert.ErrorIs(t, reserveErr(tc.err), domain.ErrUnavailable)
		})
	}
}

func TestBuy_ExecuteStageRejectionReturnsUnavailable(t *testing.T) {
	for _, tc := range breakerRejections {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRepo{reserveErr: tc.err}
			svc := newBreakerService(f)

			_, err := svc.Buy(t.Context(), 1, "u1")
			assert.ErrorIs(t, err, domain.ErrUnavailable)
			assert.ErrorIs(t, err, tc.err, "the original breaker error stays in the chain")
		})
	}
}
