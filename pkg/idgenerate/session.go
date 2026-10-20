package idgenerate

import (
	"math"
	"sync/atomic"
	"time"
)

type Session struct {
	leaseID int64
	clock
	SnowFlakeIDGen
	reservation
	lease
}

func NewSession(workerID uint64, leaseID, floorMs int64, clock clock) *Session {
	s := &Session{
		leaseID:  leaseID,
		clock:    clock,
		workerID: workerID,
	}

	if floorMs > 0 {
		s.state.Store(uint64(floorMs)<<sequenceBits | sequenceMask)
	}
	return s
}

const revokedDeadline = math.MinInt64

type lease struct {
	deadLine atomic.Int64
}

func (l *lease) extend(until time.Duration) {
	for {
		old := l.deadLine.Load()
		if old == revokedDeadline || int64(until) <= old {
			return
		}

		if l.deadLine.CompareAndSwap(old, int64(until)) {
			return
		}
	}
}

func (l *lease) revoke() {
	l.deadLine.Store(revokedDeadline)
}

func (l *lease) valid(now time.Duration) bool {
	return l.deadLine.Load() > int64(now)
}

type reservation struct {
	untilMs atomic.Int64
}

func (r *reservation) advance(untilMs int64) bool {
	for {
		cur := r.untilMs.Load()
		if cur >= untilMs {
			return false
		}

		if r.untilMs.CompareAndSwap(cur, untilMs) {
			return true
		}
	}
}

func (r *reservation) until() int64 { return r.untilMs.Load() }
func (r *reservation) coverTs(tsMs int64) bool {
	return r.untilMs.Load() >= tsMs
}

type clock struct {
	wall time.Time
	mono time.Duration
}

func newClock(wall time.Time, mono time.Duration) *clock {
	wallTime := wall.Round(0)
	return &clock{
		wall: wallTime,
		mono: mono,
	}
}

func (c clock) drift(wall time.Time, mono time.Duration) time.Duration {
	return wall.Sub(c.wall) - (mono - c.mono)
}
