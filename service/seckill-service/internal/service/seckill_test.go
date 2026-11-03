package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRepo struct {
	result     int
	err        error
	activityID int64
	userID     string
}

func (f *fakeRepo) DeductStock(_ context.Context, activityID int64, userID string) (int, error) {
	f.activityID, f.userID = activityID, userID
	return f.result, f.err
}

func newService(t *testing.T, repo *fakeRepo) SeckillService {
	t.Helper()
	svc, err := NewSeckillService(repo)
	require.NoError(t, err)
	return svc
}

func TestBuy_SuccessReturnsNil(t *testing.T) {
	repo := &fakeRepo{result: 1}
	require.NoError(t, newService(t, repo).Buy(context.Background(), 42, "u1"))
	assert.Equal(t, int64(42), repo.activityID)
	assert.Equal(t, "u1", repo.userID)
}

func TestBuy_MapsLuaResult(t *testing.T) {
	cases := []struct {
		name   string
		result int
		want   error
	}{
		{"duplicate", 2, ErrAlreadyBought},
		{"sold out", 0, ErrSoldOut},
		{"not warmed up", -1, ErrNotStarted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := newService(t, &fakeRepo{result: tc.result}).Buy(context.Background(), 1, "u1")
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestBuy_UnknownResultIsAnErrorNotSuccess(t *testing.T) {
	err := newService(t, &fakeRepo{result: 99}).Buy(context.Background(), 1, "u1")
	require.Error(t, err)
	for _, known := range []error{ErrAlreadyBought, ErrSoldOut, ErrNotStarted} {
		assert.NotErrorIs(t, err, known)
	}
}

func TestBuy_RepoErrorIsWrapped(t *testing.T) {
	boom := errors.New("redis down")
	err := newService(t, &fakeRepo{err: boom}).Buy(context.Background(), 1, "u1")
	require.ErrorIs(t, err, boom, "callers must be able to errors.Is the redis failure")
}
