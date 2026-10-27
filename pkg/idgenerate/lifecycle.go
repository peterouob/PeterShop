package idgenerate

import (
	"context"
	"time"
)

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
