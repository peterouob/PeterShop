package idgenerate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLease_Revoke(t *testing.T) {
	l := &lease{}
	l.extend(10)
	l.extend(5)

	assert.Equal(t, l.valid(9), true)
	assert.Equal(t, l.valid(10), false)
	assert.Equal(t, l.valid(1), true)

	l.revoke()
	assert.Equal(t, l.valid(1), false)
	l.extend(100)
	assert.Equal(t, l.valid(1), false, "revoke cannot be extend")
}

func TestReservation_Advance(t *testing.T) {
	r := &reservation{}
	assert.True(t, r.advance(100))
	assert.False(t, r.advance(50))
	assert.False(t, r.advance(100))
	assert.True(t, r.coverTs(100))
	assert.False(t, r.coverTs(101))
}

func TestClockAnchorDetectsJumps(t *testing.T) {
	w := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	a := newClock(w, 10*time.Second)

	cases := []struct {
		name string
		wall time.Time
		mono time.Duration
		want time.Duration
	}{
		{"in sync", w.Add(5 * time.Second), 15 * time.Second, 0},
		{"wall jumped forward", w.Add(10 * time.Second), 15 * time.Second, 5 * time.Second},
		{"wall jumped back", w.Add(2 * time.Second), 15 * time.Second, -3 * time.Second},
		{"no time elapsed", w, 10 * time.Second, 0},
		{"wall stalled", w, 15 * time.Second, -5 * time.Second},
		{"sub-second drift", w.Add(5*time.Second + time.Millisecond), 15 * time.Second, time.Millisecond},
		{"drift after long uptime", w.Add(time.Hour + 30*time.Second), time.Hour + 10*time.Second, 30 * time.Second},
		{"large forward jump", w.Add(24*time.Hour + 5*time.Second), 15 * time.Second, 24 * time.Hour},
		{"wall before anchor", w.Add(-time.Hour), 15 * time.Second, -(time.Hour + 5*time.Second)},
	}
	for _, c := range cases {
		if got := a.drift(c.wall, c.mono); got != c.want {
			t.Errorf("%s: drift = %v, want %v", c.name, got, c.want)
		}
	}
}
