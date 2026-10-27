package idgenerate

import (
	"errors"
	"math"
	"sync/atomic"
	"time"
)

const (
	leaseMargin     = 500 * time.Millisecond
	revokedDeadline = math.MinInt64
	sealedState     = math.MaxUint64
)

var (
	ErrLeaseExpired = errors.New("idgenerate: lease expired")
	ErrClosed       = errors.New("idgenerate: session closed")
)

type session struct {
	leaseID int64
	gen     generator
	lease   lease
	reserve reservation
	anchor  clockAnchor
}

func newSession(workerID uint64, leaseID, floorMs int64, anchor clockAnchor) *session {
	s := &session{leaseID: leaseID, anchor: anchor}
	s.gen.workerID = workerID
	if floorMs > 0 {
		s.gen.state.Store(uint64(floorMs)<<sequenceBits | sequenceMask)
	}
	return s
}

func (s *session) close() (int64, bool) {
	s.lease.revoke()
	old := s.gen.state.Swap(sealedState)
	if old == sealedState || old == 0 {
		return 0, false
	}
	return int64(old >> sequenceBits), true
}

func (s *session) reserveTarget(nowMs int64, window time.Duration) (int64, bool) {
	cur := s.reserve.until()
	target := nowMs + window.Milliseconds()

	if target-cur < int64(math.Ceil(float64(window.Milliseconds())/2)) {
		return cur, false
	}

	return target, true
}

func (s *session) admit(mono time.Duration) error {
	if !s.lease.valid(mono) {
		return ErrLeaseExpired
	}
	return nil
}

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
