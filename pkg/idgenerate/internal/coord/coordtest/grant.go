package coordtest

import (
	"context"
	"errors"
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

func Run(t *testing.T, newCoord func(t *testing.T) coord.Coordinator) {
	ctx := context.Background()

	t.Run("ClaimFreeID", func(t *testing.T) {
		c := newCoord(t)
		lease := grant(t, c)
		prev, ok, err := c.Claim(ctx, 1, lease, "a")
		if err != nil || !ok || prev != coord.NoReservation {
			t.Fatalf("got (%d, %v, %v)", prev, ok, err)
		}
	})

	t.Run("ClaimIsIdempotentForSameLease", func(t *testing.T) {
		c := newCoord(t)
		lease := grant(t, c)
		mustClaim(t, c, lease)
		if _, ok, err := c.Claim(ctx, 1, lease, "a"); err != nil || !ok {
			t.Fatalf("retry with same lease: ok=%v err=%v", ok, err)
		}
	})

	t.Run("ClaimOccupiedByOtherLease", func(t *testing.T) {
		c := newCoord(t)
		mustClaim(t, c, grant(t, c))
		_, ok, err := c.Claim(ctx, 1, grant(t, c), "b")
		if ok || !errors.Is(err, coord.ErrKeyHadOccupied) {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	})

	t.Run("ReserveIsFencedByLease", func(t *testing.T) {
		c := newCoord(t)
		owner := grant(t, c)
		mustClaim(t, c, owner)
		other := grant(t, c)
		if err := c.Reserve(ctx, 1, other, 123); !errors.Is(err, coord.ErrLeaseLost) {
			t.Fatalf("non-owner reserve: %v", err)
		}
		if err := c.Reserve(ctx, 1, owner, 123); err != nil {
			t.Fatalf("owner reserve: %v", err)
		}
	})

	t.Run("NextOwnerSeesReservationAfterLeaseLoss", func(t *testing.T) {
		c := newCoord(t)
		a := grant(t, c)
		mustClaim(t, c, a)
		if err := c.Reserve(ctx, 1, a, 5000); err != nil {
			t.Fatal(err)
		}
		if err := c.Revoke(ctx, a); err != nil {
			t.Fatal(err)
		}
		prev, ok, err := c.Claim(ctx, 1, grant(t, c), "b")
		if err != nil || !ok || prev != 5000 {
			t.Fatalf("got (%d, %v, %v)", prev, ok, err)
		}
	})

	t.Run("ReleaseWritesLastTsAndFreesID", func(t *testing.T) {
		c := newCoord(t)
		a := grant(t, c)
		mustClaim(t, c, a)
		if err := c.Reserve(ctx, 1, a, 9000); err != nil {
			t.Fatal(err)
		}
		if err := c.Release(ctx, 1, a, 4000); err != nil {
			t.Fatal(err)
		}
		prev, ok, err := c.Claim(ctx, 1, grant(t, c), "b")
		if err != nil || !ok || prev != 4000 {
			t.Fatalf("got (%d, %v, %v)", prev, ok, err)
		}
	})

	t.Run("ReleaseByNonOwnerIsNoop", func(t *testing.T) {
		c := newCoord(t)
		mustClaim(t, c, grant(t, c))
		if err := c.Release(ctx, 1, grant(t, c), 1); err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.Claim(ctx, 1, grant(t, c), "c"); !errors.Is(err, coord.ErrKeyHadOccupied) {
			t.Fatalf("id should still be held: %v", err)
		}
	})

	t.Run("KeepAliveAfterRevoke", func(t *testing.T) {
		c := newCoord(t)
		lease := grant(t, c)
		if err := c.Revoke(ctx, lease); err != nil {
			t.Fatal(err)
		}
		if _, err := c.KeepAlive(ctx, lease); !errors.Is(err, coord.ErrLeaseNotFound) {
			t.Fatalf("got %v", err)
		}
		if err := c.Revoke(ctx, lease); !errors.Is(err, coord.ErrLeaseNotFound) {
			t.Fatalf("second revoke: got %v, want ErrLeaseNotFound", err)
		}
	})
}

func grant(t *testing.T, c coord.Coordinator) int64 {
	t.Helper()
	g, err := c.Grant(context.Background(), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return g.ID
}

func mustClaim(t *testing.T, c coord.Coordinator, leaseID int64) {
	t.Helper()
	if _, ok, err := c.Claim(context.Background(), 1, leaseID, "owner"); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
}
