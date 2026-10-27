package idgenerate

import (
	"context"
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
	ErrLeaseExpired         = errors.New("idgenerate session: lease expired")
	ErrReservationExhausted = errors.New("idgenerate session: reservation exhausted")
	ErrClosed               = errors.New("idgenerate session: session closed")
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

type (
	keepAlive func(ctx context.Context, leaseID int64) (time.Duration, error)
	reserve   func(ctx context.Context, workerID uint64, leaseID, utilMS int64) error
)

func (s *Session) KeepAlive(ctx context.Context, keepAlive keepAlive) error {
	sendAt := s.clock.mono()
	ttl, err := keepAlive(ctx, s.leaseID)
	if err != nil {
		return err
	}

	s.lease.extend(sendAt + ttl - leaseMargin)

	return nil
}

func (s *Session) ReNew(ctx context.Context, window time.Duration, reserve reserve) error {
	until, needed := s.reserveTarget(window)
	if !needed {
		return nil
	}

	if err := reserve(ctx, s.workerID, s.leaseID, until); err != nil {
		return err
	}

	s.reservation.advance(until)
	return nil
}

func (s *Session) Close() (int64, bool) {
	s.lease.revoke()
	old := s.state.Swap(sealedState)
	if old == sealedState || old == 0 {
		return 0, false
	}
	return int64(old >> sequenceBits), true
}

func (s *Session) reserveTarget(window time.Duration) (int64, bool) {
	cur := s.reservation.until()
	target := s.clock.wallMs() + window.Milliseconds()

	if target-cur < int64(math.Ceil(float64(window.Milliseconds())/2)) {
		return cur, false
	}

	return target, true
}

func (s *Session) admit(tsMs int64) error {
	if !s.lease.valid(s.clock.mono()) {
		return ErrLeaseExpired
	}

	if !s.reservation.coverTs(tsMs) {
		return ErrReservationExhausted
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

type clock struct {
	now        func() time.Time
	originTime time.Time
}

func newClock(now func() time.Time) clock {
	if now == nil {
		now = time.Now
	}

	return clock{now: now, originTime: now()}
}

func (c clock) wallMs() int64 {
	return c.now().Round(0).UnixMilli()
}

func (c clock) mono() time.Duration {
	return c.now().Sub(c.originTime)
}

func (c clock) drift() time.Duration {
	t := c.now()
	return t.Round(0).Sub(c.originTime.Round(0)) - t.Sub(c.originTime)
}
