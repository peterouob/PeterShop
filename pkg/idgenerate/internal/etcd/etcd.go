package etcd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Coordinator struct {
	client *clientv3.Client
	prefix string
}

var _ coord.Coordinator = (*Coordinator)(nil)

func New(client *clientv3.Client, prefix string) *Coordinator {
	return &Coordinator{client: client, prefix: prefix}
}

func (c *Coordinator) Grant(ctx context.Context, ttl time.Duration) (*coord.LeaseGrantResp, error) {
	if ttl <= 0 {
		return nil, coord.ErrTTLBeZero
	}

	sec := max(int64(math.Ceil(ttl.Seconds())), 1)

	r, err := c.client.Grant(ctx, sec)
	if err != nil {
		return nil, fmt.Errorf("grant: type:%w, error: %w", coord.ErrGrantLease, err)
	}

	d := time.Duration(r.TTL) * time.Second

	resp := &coord.LeaseGrantResp{
		ID:  int64(r.ID),
		TTL: d,
	}

	return resp, nil
}

func (c *Coordinator) KeepAlive(ctx context.Context, leaseID int64) (time.Duration, error) {
	resp, err := c.client.KeepAliveOnce(ctx, clientv3.LeaseID(leaseID))
	if err != nil {
		if errors.Is(err, rpctypes.ErrLeaseNotFound) {
			return 0, coord.ErrLeaseNotFound
		}
		return 0, err
	}
	return time.Duration(resp.TTL) * time.Second, nil
}

func (c *Coordinator) Revoke(ctx context.Context, leaseID int64) error {
	_, err := c.client.Revoke(ctx, clientv3.LeaseID(leaseID))

	if err != nil {
		if errors.Is(err, rpctypes.ErrLeaseNotFound) {
			return coord.ErrLeaseNotFound
		}
		return err
	}

	return nil
}

func (c *Coordinator) Claim(ctx context.Context, workerID uint64, leaseID int64, owner string) (int64, bool, error) {
	key, rKey := c.workerKey(workerID), c.reserveKey(workerID)
	lid := clientv3.LeaseID(leaseID)

	resp, err := c.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, owner, clientv3.WithLease(lid)), clientv3.OpGet(rKey)).
		Else(clientv3.OpGet(key), clientv3.OpGet(rKey)).Commit()

	if err != nil {
		if errors.Is(err, rpctypes.ErrLeaseNotFound) {
			return coord.NoReservation, false, coord.ErrLeaseNotFound
		}
		return coord.NoReservation, false, err
	}

	if !resp.Succeeded {
		cur := resp.Responses[0].GetResponseRange().Kvs
		if len(cur) == 0 || clientv3.LeaseID(cur[0].Lease) != lid {
			return coord.NoReservation, false, coord.ErrKeyHadOccupied
		}
	}

	kvs := resp.Responses[1].GetResponseRange().Kvs
	if len(kvs) == 0 {
		return coord.NoReservation, true, nil
	}

	prev, err := strconv.ParseInt(string(kvs[0].Value), 10, 64)
	if err != nil {
		_ = c.Release(ctx, workerID, leaseID, 0)
		return coord.NoReservation, false, fmt.Errorf("parse int error: %w", err)
	}

	return prev, true, nil
}

func (c *Coordinator) Release(ctx context.Context, workerID uint64, leaseID int64, lastMs int64) error {
	key := c.workerKey(workerID)
	ops := []clientv3.Op{clientv3.OpDelete(key)}
	if lastMs > 0 {
		ops = append([]clientv3.Op{
			clientv3.OpPut(c.reserveKey(workerID), strconv.FormatInt(lastMs, 10)),
		}, ops...)
	}

	if _, err := c.client.Txn(ctx).
		If(clientv3.Compare(clientv3.LeaseValue(key), "=", clientv3.LeaseID(leaseID))).
		Then(ops...).
		Commit(); err != nil {
		if errors.Is(err, rpctypes.ErrLeaseNotFound) {
			return coord.ErrLeaseNotFound
		}
		return err
	}

	return nil
}

func (c *Coordinator) Reserve(ctx context.Context, workerID uint64, leaseID int64, untilMs int64) error {
	resp, err := c.client.Txn(ctx).If(clientv3.Compare(clientv3.LeaseValue(c.workerKey(workerID)), "=", clientv3.LeaseID(leaseID))).
		Then(clientv3.OpPut(c.reserveKey(workerID), strconv.FormatInt(untilMs, 10))).
		Commit()

	if err != nil {
		if errors.Is(err, rpctypes.ErrLeaseNotFound) {
			return coord.ErrLeaseNotFound
		}
		return err
	}

	if !resp.Succeeded {
		return coord.ErrLeaseLost
	}
	return nil
}

func (c *Coordinator) workerKey(id uint64) string  { return fmt.Sprintf("%s/workers/%d", c.prefix, id) }
func (c *Coordinator) reserveKey(id uint64) string { return fmt.Sprintf("%s/last_ts/%d", c.prefix, id) }
