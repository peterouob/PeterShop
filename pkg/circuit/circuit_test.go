package circuit

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/require"
)

var errBoom = errors.New("boom")

func newTestGate(t *testing.T, maxRequest uint32) *Gate[struct{}] {
	t.Helper()
	g := NewGate[struct{}](gobreaker.Settings{
		Name:        "test",
		MaxRequests: maxRequest,
		Timeout:     20 * time.Millisecond,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= 1
		},
	})
	return g
}

func fail(g *Gate[struct{}]) {
	_, _ = g.Breaker().Execute(func() (struct{}, error) {
		return struct{}{}, errBoom
	})
}

func succeed(g *Gate[struct{}]) error {
	_, err := g.Breaker().Execute(func() (struct{}, error) {
		return struct{}{}, nil
	})
	return err
}

func toHalfOpen(t *testing.T, g *Gate[struct{}]) {
	t.Helper()
	fail(g)
	require.Equal(t, gobreaker.StateOpen, g.Breaker().State())
	require.Eventually(t, func() bool {
		return g.Breaker().State() == gobreaker.StateHalfOpen
	}, time.Second, 2*time.Millisecond)
}

func TestNewGate_RejectsTooManyProbes(t *testing.T) {
	g := NewGate[struct{}](gobreaker.Settings{MaxRequests: probeMask + 1})
	require.Equal(t, uint64(1), g.maxProbes)
}

func TestAdmit(t *testing.T) {
	g := newTestGate(t, 3)
	t.Run("admit allow", func(t *testing.T) {
		for range 100 {
			_, ok := g.Admit()
			require.True(t, ok)
		}
	})

	t.Run("admit reject", func(t *testing.T) {
		fail(g)
		for range 100 {
			_, ok := g.Admit()
			require.False(t, ok)
		}
	})

	t.Run("admit half open", func(t *testing.T) {
		toHalfOpen(t, g)
		p1, ok := g.Admit()
		require.True(t, ok)
		p2, ok2 := g.Admit()
		require.True(t, ok2)
		_, ok3 := g.Admit()
		require.True(t, ok3)
		_, ok4 := g.Admit()
		require.False(t, ok4, "the request is full")
		p1.Release()
		_, ok4 = g.Admit()
		require.True(t, ok4)
		p2.MarkUsed()
		p2.Release()
		_, ok5 := g.Admit()
		require.False(t, ok5, "makr used should not release the people")
	})
}

func TestGate_ReleaseIsIdempotent(t *testing.T) {
	g := newTestGate(t, 1)
	toHalfOpen(t, g)

	p, ok := g.Admit()
	require.True(t, ok)
	p.Release()
	p.Release()

	q, ok := g.Admit()
	require.True(t, ok)
	_, ok = g.Admit()
	require.False(t, ok)
	q.Release()
}

func TestGate_StalePermitDoesNotAffectNewEpoch(t *testing.T) {
	g := newTestGate(t, 1)
	toHalfOpen(t, g)
	stale, ok := g.Admit()
	require.True(t, ok)

	fail(g)
	require.Eventually(t, func() bool {
		return g.Breaker().State() == gobreaker.StateHalfOpen
	}, time.Second, 2*time.Millisecond)

	_, ok = g.Admit()
	require.True(t, ok)

	stale.Release()
	_, ok = g.Admit()
	require.False(t, ok)
}

func TestGate_RecoversToClosed(t *testing.T) {
	const max = 3
	g := newTestGate(t, max)
	toHalfOpen(t, g)

	for range max {
		p, ok := g.Admit()
		require.True(t, ok)
		p.MarkUsed()
		require.NoError(t, succeed(g))
	}
	require.Equal(t, gobreaker.StateClosed, g.Breaker().State())
}

func TestGate_Concurrent(t *testing.T) {
	const max = 5
	g := newTestGate(t, max)
	toHalfOpen(t, g)

	var inFlight, peak atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			for range 100 {
				p, ok := g.Admit()
				if !ok {
					continue
				}
				n := inFlight.Add(1)
				for {
					cur := peak.Load()
					if n <= cur || peak.CompareAndSwap(cur, n) {
						break
					}
				}
				runtime.Gosched()
				inFlight.Add(-1)
				p.Release()
			}
		})
	}

	wg.Wait()
	require.LessOrEqual(t, peak.Load(), int64(max), "half-open must never admit more than max probes at once")
	require.Positive(t, peak.Load())
}
