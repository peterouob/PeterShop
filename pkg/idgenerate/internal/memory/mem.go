package memory

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
)

var _ coord.Coordinator = (*Coordinator)(nil)

type Coordinator struct {
	mu  sync.Mutex
	now func() time.Time

	nextLeaseID int64
	leases      map[int64]*lease
	workers     map[uint64]worker
	lastTs      map[uint64]int64
}

type lease struct {
	ttl      time.Duration
	expireAt time.Time
	workers  map[uint64]struct{}
}

type worker struct {
	owner   string
	leaseID int64
}

func New() *Coordinator { return NewWithClock(time.Now) }

func NewWithClock(now func() time.Time) *Coordinator {
	return &Coordinator{
		now:     now,
		leases:  make(map[int64]*lease),
		workers: make(map[uint64]worker),
		lastTs:  make(map[uint64]int64),
	}
}

func (c *Coordinator) Grant(ctx context.Context, ttl time.Duration) (*coord.LeaseGrantResp, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d := time.Duration(max(int64(math.Ceil(ttl.Seconds())), 1)) * time.Second

	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()

	c.nextLeaseID++
	id := c.nextLeaseID
	c.leases[id] = &lease{ttl: d, expireAt: c.now().Add(d), workers: make(map[uint64]struct{})}
	return &coord.LeaseGrantResp{ID: id, TTL: d}, nil
}

func (c *Coordinator) KeepAlive(ctx context.Context, leaseID int64) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()

	l, ok := c.leases[leaseID]
	if !ok {
		return 0, coord.ErrLeaseNotFound
	}
	l.expireAt = c.now().Add(l.ttl)
	return l.ttl, nil
}

func (c *Coordinator) Revoke(leaseID int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()

	if l, ok := c.leases[leaseID]; ok {
		c.dropLeaseLocked(leaseID, l)
	}
	return nil
}

func (c *Coordinator) Claim(req coord.CoordinatorReq, owner string) (int64, bool, error) {
	if err := req.Ctx.Err(); err != nil {
		return coord.NoReservation, false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()

	if w, ok := c.workers[req.WorkerID]; ok {
		if w.leaseID != req.LeaseID {
			return coord.NoReservation, false, coord.ErrKeyHadOccupied
		}
	} else {
		l, ok := c.leases[req.LeaseID]
		if !ok {
			return coord.NoReservation, false, coord.ErrLeaseNotFound
		}
		c.workers[req.WorkerID] = worker{owner: owner, leaseID: req.LeaseID}
		l.workers[req.WorkerID] = struct{}{}
	}

	prev, ok := c.lastTs[req.WorkerID]
	if !ok {
		return coord.NoReservation, true, nil
	}
	return prev, true, nil
}

func (c *Coordinator) Release(req coord.CoordinatorReq, lastMs int64) error {
	if err := req.Ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()

	if !c.ownsLocked(req.WorkerID, req.LeaseID) {
		return nil
	}
	if lastMs > 0 {
		c.lastTs[req.WorkerID] = lastMs
	}
	c.deleteWorkerLocked(req.WorkerID)
	return nil
}

func (c *Coordinator) Reserve(req coord.CoordinatorReq, untilMs int64) error {
	if err := req.Ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()

	if !c.ownsLocked(req.WorkerID, req.LeaseID) {
		return coord.ErrLeaseLost
	}
	c.lastTs[req.WorkerID] = untilMs
	return nil
}

func (c *Coordinator) ownsLocked(workerID uint64, leaseID int64) bool {
	w, ok := c.workers[workerID]
	return ok && w.leaseID == leaseID
}

func (c *Coordinator) expireLocked() {
	now := c.now()
	for id, l := range c.leases {
		if !now.Before(l.expireAt) {
			c.dropLeaseLocked(id, l)
		}
	}
}

func (c *Coordinator) dropLeaseLocked(id int64, l *lease) {
	for wid := range l.workers {
		delete(c.workers, wid)
	}
	delete(c.leases, id)
}

func (c *Coordinator) deleteWorkerLocked(workerID uint64) {
	w, ok := c.workers[workerID]
	if !ok {
		return
	}
	if l, ok := c.leases[w.leaseID]; ok {
		delete(l.workers, workerID)
	}
	delete(c.workers, workerID)
}
