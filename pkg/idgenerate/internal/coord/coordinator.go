package coord

import (
	"context"
	"time"
)

const NoReservation = -1

type CoordinatorReq struct {
	Ctx      context.Context
	WorkerID uint64
	LeaseID  int64
}

type Coordinator interface {
	Grant(ctx context.Context, ttl time.Duration) (*LeaseGrantResp, error)
	KeepAlive(ctx context.Context, leaseID int64) (time.Duration, error)
	Revoke(leaseID int64) error
	Claim(req CoordinatorReq, owner string) (int64, bool, error)
	Release(req CoordinatorReq, lastMs int64) error
	Reserve(req CoordinatorReq, utilMs int64) error
}
