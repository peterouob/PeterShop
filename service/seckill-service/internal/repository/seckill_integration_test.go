//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeductStock_OneOrderPerUserPerActivity(t *testing.T) {
	ctx := context.Background()
	rdb := testkit.OpenRedis(t)
	repo := NewSeckillRepo(rdb, nil)

	const a1, a2 int64 = 1, 2
	for _, aid := range []int64{a1, a2} {
		require.NoError(t, rdb.Set(ctx, rediskey.Stock(aid), 10, 0).Err())
	}

	got, err := repo.DeductStock(ctx, a1, "u1")
	require.NoError(t, err)
	assert.Equal(t, 1, got, "u1 first purchase in activity 1 should succeed")

	got, err = repo.DeductStock(ctx, a1, "u1")
	require.NoError(t, err)
	assert.Equal(t, 2, got, "u1 repeated purchase in activity 1 should be blocked")

	got, err = repo.DeductStock(ctx, a2, "u1")
	require.NoError(t, err)
	assert.Equal(t, 1, got, "same user in the next activity (D009) should not be blocked")

	isMember, err := rdb.SIsMember(ctx, rediskey.Users(a1), "u1").Result()
	require.NoError(t, err)
	assert.True(t, isMember, "u1 should be a member of %s", rediskey.Users(a1))
}
