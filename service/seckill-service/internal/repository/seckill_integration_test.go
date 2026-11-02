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

func TestDeductStock_OneOrderPerUserPerProduct(t *testing.T) {
	ctx := context.Background()
	rdb := testkit.OpenRedis(t)
	repo := NewSeckillRepo(rdb, nil)

	for _, pid := range []string{"p1", "p2"} {
		require.NoError(t, rdb.Set(ctx, rediskey.Stock(pid), 10, 0).Err())
	}

	got, err := repo.DeductStock(ctx, "p1", "u1")
	require.NoError(t, err)
	assert.Equal(t, 1, got, "u1 first purchase of p1 should succeed")

	got, err = repo.DeductStock(ctx, "p1", "u1")
	require.NoError(t, err)
	assert.Equal(t, 2, got, "u1 repeated purchase of p1 should be blocked")

	got, err = repo.DeductStock(ctx, "p2", "u1")
	require.NoError(t, err)
	assert.Equal(t, 1, got, "same user purchasing a different product should not be blocked")

	isMember, err := rdb.SIsMember(ctx, rediskey.Users("p1"), "u1").Result()
	require.NoError(t, err)
	assert.True(t, isMember, "u1 should be a member of %s", rediskey.Users("p1"))
}
