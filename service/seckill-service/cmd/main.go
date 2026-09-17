package main

import (
	"github.com/joho/godotenv"
	"github.com/peterouob/seckill_service/api/seckillproto"
	"github.com/peterouob/seckill_service/pkg/cache"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/database"
	"github.com/peterouob/seckill_service/pkg/etcd"
	"github.com/peterouob/seckill_service/pkg/leader"
	"github.com/peterouob/seckill_service/pkg/logger"
	"github.com/peterouob/seckill_service/pkg/mq/kafka"
	transport "github.com/peterouob/seckill_service/pkg/transport/grpc"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/seckillgrpc"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/service"
	"go.uber.org/fx"
	"google.golang.org/grpc"
)

const serviceName = "seckill-service"

var seckillModule = fx.Module(serviceName,
	fx.Provide(
		repository.NewSeckillRepo,
		service.NewSeckillService,
		seckillgrpc.NewHandler,
	),
	fx.Invoke(func(srv *grpc.Server, h *seckillgrpc.Handler) {
		seckillproto.RegisterSeckillServiceServer(srv, h)
	}),
)

func main() {
	_ = godotenv.Load()
	logger.InitLogger(serviceName)

	fx.New(
		fx.Provide(func() (*config.Config, error) {
			return config.Load(serviceName, config.SectionMySQL, config.SectionRedis, config.SectionKafka, config.SectionEtcd)
		}),
		database.Module,
		cache.Module,
		etcd.Module,
		leader.Module,
		kafka.ProducerModule,
		transport.GrpcServerModule,
		seckillModule,
	).Run()
}
