package etcd

import (
	"context"
	"fmt"

	"github.com/peterouob/seckill_service/pkg/config"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/fx"
)

func newClient(cfg *config.Config) (*clientv3.Client, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Etcd.Endpoints,
		DialTimeout: cfg.Etcd.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("etcd: connect to %v: %w", cfg.Etcd.Endpoints, err)
	}
	return client, nil
}

var Module = fx.Module("etcd",
	fx.Provide(func(lc fx.Lifecycle, cfg *config.Config) (*clientv3.Client, error) {
		client, err := newClient(cfg)
		if err != nil {
			return nil, err
		}
		lc.Append(fx.Hook{
			OnStop: func(context.Context) error { return client.Close() },
		})
		return client, nil
	}),
)
