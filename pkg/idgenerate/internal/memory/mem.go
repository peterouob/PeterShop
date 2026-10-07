package memory

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
)

type MemLease struct {
	mu     sync.Mutex
	nextID int64
	leases map[int64]time.Time
	now    func() time.Time
}

var _ coord.Coordinator = (*MemLease)(nil)

func NewMemLease() *MemLease {
	return &MemLease{
		leases: map[int64]time.Time{},
		now:    time.Now,
	}
}

func (m *MemLease) Grant(ctx context.Context, ttl time.Duration) (*coord.LeaseGrantResp, error) {
	if ctx.Err() != nil {
		return nil, coord.ErrorGrantLease
	}

	sec := max(int64(math.Ceil(ttl.Seconds())), 1)
	d := time.Duration(sec) * time.Second

	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID++
	m.leases[m.nextID] = m.now().Add(d)
	return &coord.LeaseGrantResp{
		ID:  m.nextID,
		TTL: d,
	}, nil
}
