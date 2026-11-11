package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepo struct {
	inserted  []int64
	insertErr error
}

func orderNos(evs []event.OrderReserved) []int64 {
	nos := make([]int64, 0, len(evs))
	for _, e := range evs {
		nos = append(nos, e.OrderNo)
	}
	return nos
}

func (f *fakeRepo) InsertPendingOrders(_ context.Context, evs []event.OrderReserved, _ time.Duration) error {
	f.inserted = append(f.inserted, orderNos(evs)...)
	return f.insertErr
}

func newService() (*orderService, *fakeRepo) {
	repo := &fakeRepo{}
	return &orderService{repo: repo, payTimeout: 15 * time.Minute}, repo
}

func reserved(orderNo int64) event.OrderReserved {
	return event.OrderReserved{OrderNo: orderNo, UserID: "u", ProductID: "p1", ActivityID: 1, Amount: 100,
		CreatedAtMs: time.Now().UnixMilli()}
}

func TestCreateOrders_InsertsBatch(t *testing.T) {
	s, repo := newService()

	require.NoError(t, s.CreateOrders(t.Context(), []event.OrderReserved{reserved(1), reserved(2)}))

	assert.Equal(t, []int64{1, 2}, repo.inserted)
}

func TestCreateOrders_InsertFailReturnsError(t *testing.T) {
	s, repo := newService()
	repo.insertErr = errors.New("pg down")

	err := s.CreateOrders(t.Context(), []event.OrderReserved{reserved(1)})

	require.ErrorIs(t, err, repo.insertErr)
}
