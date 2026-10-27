package coord

import (
	"context"
	"time"
)

const NoReservation = -1

type Coordinator interface {
	Grant(ctx context.Context, ttl time.Duration) (*LeaseGrantResp, error)
	KeepAlive(ctx context.Context, leaseID int64) (time.Duration, error)
	Revoke(ctx context.Context, leaseID int64) error
	Claim(ctx context.Context, workerID uint64, leaseID int64, owner string) (prevMs int64, ok bool, err error)
	Release(ctx context.Context, workerID uint64, leaseID int64, lastMs int64) error
	Reserve(ctx context.Context, workerID uint64, leaseID int64, untilMs int64) error
}
