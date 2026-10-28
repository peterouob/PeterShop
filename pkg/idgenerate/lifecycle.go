package idgenerate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
	"github.com/peterouob/seckill_service/pkg/logger"
)

const (
	leaseTTL      = 10 * time.Second
	safetyMargin  = 2 * time.Second
	maxWaitTime   = 20 * time.Millisecond
	revokeTimeout = 30 * time.Second
	reserveAhead  = 3 * time.Second
)

var (
	ErrWorkerIDExhaust = errors.New("lifecycle: not enough workerID to claim")
	ErrClaimSnowflake  = errors.New("lifecycle: claim snowflake")
)

func (sf *Snowflake) start(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, leaseTTL-safetyMargin)
	defer cancel()

	now := sf.clock.mono()
	resp, err := sf.coord.Grant(ctx, leaseTTL)
	if err != nil {
		return err
	}

	s, err := sf.claim(ctx, resp.ID)
	if err != nil {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokeTimeout)
		defer cancel()
		_ = sf.coord.Revoke(rctx, resp.ID)
		return fmt.Errorf("type: %w, err: %w", ErrClaimSnowflake, err)
	}
	s.lease.extend(now + resp.TTL - safetyMargin)
	sf.cur.Store(s)
	return nil
}

func (sf *Snowflake) claim(ctx context.Context, leaseID int64) (*session, error) {
	occupied, err := sf.coord.Occupied(ctx)
	if err != nil {
		return nil, err
	}

	for id := range uint64(maxWorkerID + 1) {
		if slices.Contains(occupied, id) {
			continue
		}

		prevMs, ok, err := sf.coord.Claim(ctx, id, leaseID, GetHostName())
		if err != nil {
			if errors.Is(err, coord.ErrKeyHadOccupied) {
				continue
			}
			return nil, err
		}

		if !ok {
			continue
		}

		wait, err := sf.wait(ctx, prevMs)
		if err != nil {
			return nil, err
		}

		if wait {
			_ = sf.coord.Release(ctx, id, leaseID, 0)
			continue
		}

		lastMs := int64(0)
		if prevMs != coord.NoReservation {
			lastMs = prevMs
		}

		s := newSession(id, leaseID, lastMs, newClockAnchor(time.UnixMilli(lastMs), sf.clock.mono()))
		if err := sf.reserve(ctx, s); err != nil {
			return nil, err
		}
		return s, nil
	}

	return nil, fmt.Errorf("%w: all %d worker ids occupied", ErrWorkerIDExhaust, maxWorkerID+1)
}

func (sf *Snowflake) wait(ctx context.Context, prevMs int64) (bool, error) {
	if prevMs == coord.NoReservation {
		return false, nil
	}
	wait := time.Duration(prevMs-sf.clock.wallMs()) * time.Millisecond
	if wait <= 0 {
		return false, nil
	} else if wait > maxWaitTime {
		return true, nil
	}

	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
		return false, nil
	case <-ctx.Done():
		return true, ctx.Err()
	}
}

func (sf *Snowflake) keepAlive(ctx context.Context, s *session) error {
	sendAt := sf.clock.mono()
	ttl, err := sf.coord.KeepAlive(ctx, s.leaseID)
	if err != nil {
		return err
	}

	s.lease.extend(sendAt + ttl - leaseMargin)

	return nil
}

func (sf *Snowflake) renew(ctx context.Context, s *session, window time.Duration) error {
	until, needed := s.reserveTarget(sf.clock.wallMs(), window)
	if !needed {
		return nil
	}

	if err := sf.coord.Reserve(ctx, s.gen.workerID, s.leaseID, until); err != nil {
		return err
	}

	s.reserve.advance(until)
	return nil
}

func (sf *Snowflake) reserve(ctx context.Context, s *session) error {
	until := sf.clock.wallMs() + reserveAhead.Milliseconds()
	if until <= s.reserve.until() {
		return nil
	}

	if err := sf.coord.Reserve(ctx, s.gen.workerID, s.leaseID, until); err != nil {
		return err
	}

	_ = s.reserve.advance(until)
	return nil
}

var (
	hostName string
	hostOnce sync.Once
)

func GetHostName() string {
	hostOnce.Do(func() {
		var err error
		hostName, err = os.Hostname()
		if err != nil {
			logger.Errorf(err, "get hostName failed")
		}
	})
	return hostName
}
