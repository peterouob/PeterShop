package idgenerate

import (
	"context"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/fx"
)

var SnowflakeProvider = fx.Module("snowflake",
	fx.Provide(func(lc fx.Lifecycle, client *clientv3.Client) (*Snowflake, error) {
		coord := NewEtcdCoordinator(client, "/idgen")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		sf, err := New(ctx, coord)
		if err != nil {
			return nil, err
		}

		lc.Append(fx.StopHook(sf.Close))
		return sf, nil
	}),
)
