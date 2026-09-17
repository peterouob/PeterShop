package main

import (
	"github.com/joho/godotenv"
	"github.com/peterouob/seckill_service/api/userproto"
	"github.com/peterouob/seckill_service/pkg/auth"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/database"
	"github.com/peterouob/seckill_service/pkg/logger"
	transport "github.com/peterouob/seckill_service/pkg/transport/grpc"
	"github.com/peterouob/seckill_service/service/user-service/internal/repository"
	"github.com/peterouob/seckill_service/service/user-service/internal/service"
	"github.com/peterouob/seckill_service/service/user-service/internal/usergrpc"
	"go.uber.org/fx"
	"google.golang.org/grpc"
)

const serviceName = "user-service"

var userModule = fx.Module(serviceName,
	fx.Provide(
		repository.NewUserRepo,
		service.NewUserService,
		usergrpc.NewHandler,
	),
	fx.Invoke(func(srv *grpc.Server, h *usergrpc.Handler) {
		userproto.RegisterUserServiceServer(srv, h)
	}),
)

func main() {
	_ = godotenv.Load()
	logger.InitLogger(serviceName)

	fx.New(
		fx.Provide(func() (*config.Config, error) {
			return config.Load(serviceName, config.SectionMySQL, config.SectionJWT)
		}),
		auth.Module,
		database.Module,
		transport.GrpcServerModule,
		userModule,
	).Run()
}
