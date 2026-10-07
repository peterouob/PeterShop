package coordtest

import (
	"context"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
	"github.com/stretchr/testify/assert"
)

func RunGrantTest(t *testing.T, newCoord func(t *testing.T) coord.Coordinator) {
	ctx := context.Background()
	ts := 5 * time.Second

	t.Run("grant return not zero", func(t *testing.T) {
		c := newCoord(t)
		resp, err := c.Grant(ctx, ts)
		assert.NoError(t, err)
		assert.NotZero(t, resp.ID)
	})

	t.Run("grant not the same", func(t *testing.T) {
		c := newCoord(t)
		seen := map[int64]bool{}

		for range 100 {
			resp, err := c.Grant(ctx, ts)
			assert.NoError(t, err)
			assert.NotZero(t, resp.ID)
			assert.False(t, seen[resp.ID])
			seen[resp.ID] = true
		}
	})
}
