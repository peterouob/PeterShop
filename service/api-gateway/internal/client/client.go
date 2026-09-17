package client

import (
	"github.com/peterouob/seckill_service/api/seckillproto"
	"github.com/peterouob/seckill_service/api/userproto"
	"github.com/peterouob/seckill_service/pkg/config"
	transport "github.com/peterouob/seckill_service/pkg/transport/grpc"
	"go.uber.org/fx"
)

func newUserClient(lc fx.Lifecycle, cfg *config.Config) (userproto.UserServiceClient, error) {
	conn, err := transport.NewConn(lc, cfg.Upstreams.User)
	if err != nil {
		return nil, err
	}
	return userproto.NewUserServiceClient(conn), nil
}

func newSeckillClient(lc fx.Lifecycle, cfg *config.Config) (seckillproto.SeckillServiceClient, error) {
	conn, err := transport.NewConn(lc, cfg.Upstreams.Seckill)
	if err != nil {
		return nil, err
	}
	return seckillproto.NewSeckillServiceClient(conn), nil
}

var Module = fx.Module("grpc-clients",
	fx.Provide(
		newUserClient,
		newSeckillClient,
	),
)
