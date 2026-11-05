package service

import (
	"context"
	"errors"
	"testing"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepo struct {
	err        error
	activityID int64
	userID     string
}

func (f *fakeRepo) DeductStock(_ context.Context, activityID int64, userID string) error {
	f.activityID, f.userID = activityID, userID
	return f.err
}

func newService(t *testing.T, repo *fakeRepo) SeckillService {
	t.Helper()
	svc, err := NewSeckillService(repo)
	require.NoError(t, err)
	return svc
}

func TestBuy_SuccessReturnsNil(t *testing.T) {
	repo := &fakeRepo{}
	require.NoError(t, newService(t, repo).Buy(context.Background(), 42, "u1"))
	assert.Equal(t, int64(42), repo.activityID)
	assert.Equal(t, "u1", repo.userID)
}

func TestBuy_RepoErrorIsWrapped(t *testing.T) {
	for _, want := range []error{domain.ErrSoldOut, errors.New("redis down")} {
		err := newService(t, &fakeRepo{err: want}).Buy(context.Background(), 1, "u1")
		require.ErrorIs(t, err, want, "callers must still be able to errors.Is the cause")
		assert.Contains(t, err.Error(), "activity=1")
	}
}
