package seckillgrpc

import (
	"context"
	"testing"

	"github.com/peterouob/seckill_service/api/seckillproto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeSeckill struct {
	called     bool
	userID     string
	activityID int64
}

func (f *fakeSeckill) Buy(_ context.Context, activityID int64, userID string) error {
	f.called, f.userID, f.activityID = true, userID, activityID
	return nil
}

func TestSeckill_ParsesActivityIDFromProductIDField(t *testing.T) {
	fake := &fakeSeckill{}
	_, err := NewHandler(fake).Seckill(context.Background(),
		&seckillproto.SeckillRequest{UserId: "u1", ProductId: "1852301234567890123"})

	require.NoError(t, err)
	assert.True(t, fake.called)
	assert.Equal(t, "u1", fake.userID)
	assert.Equal(t, int64(1852301234567890123), fake.activityID)
}

func TestSeckill_RejectsInvalidActivityID(t *testing.T) {
	cases := []struct {
		name string
		in   *seckillproto.SeckillRequest
	}{
		{"missing user", &seckillproto.SeckillRequest{ProductId: "1"}},
		{"missing activity", &seckillproto.SeckillRequest{UserId: "u1"}},
		{"not a number", &seckillproto.SeckillRequest{UserId: "u1", ProductId: "p1"}},
		{"zero", &seckillproto.SeckillRequest{UserId: "u1", ProductId: "0"}},
		{"negative one", &seckillproto.SeckillRequest{UserId: "u1", ProductId: "-1"}},
		{"any other negative", &seckillproto.SeckillRequest{UserId: "u1", ProductId: "-2"}},
		{"overflows int64", &seckillproto.SeckillRequest{UserId: "u1", ProductId: "9223372036854775808"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSeckill{}
			_, err := NewHandler(fake).Seckill(context.Background(), tc.in)

			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.False(t, fake.called, "invalid input must not reach Buy")
		})
	}
}
