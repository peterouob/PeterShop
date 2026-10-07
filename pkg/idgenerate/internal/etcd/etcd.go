package etcd

import (
	"context"
	"math"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Coordinator struct {
	client *clientv3.Client
	prefix string
}

func New(client *clientv3.Client, prefix string) *Coordinator {
	return &Coordinator{client: client, prefix: prefix}
}

func (c *Coordinator) Grant(ctx context.Context, ttl time.Duration) (*coord.LeaseGrantResp, error) {
	sec := max(int64(math.Ceil(ttl.Seconds())), 1)

	r, err := c.client.Grant(ctx, sec)
	if err != nil {
		return nil, coord.ErrorGrantLease
	}

	d := time.Duration(r.TTL) * time.Second

	resp := &coord.LeaseGrantResp{
		ID:  int64(r.ID),
		TTL: d,
	}

	return resp, nil
}
