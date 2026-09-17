package main

import (
	"github.com/joho/godotenv"
	"github.com/peterouob/seckill_service/pkg/auth"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/logger"
	transport "github.com/peterouob/seckill_service/pkg/transport/http"
	"github.com/peterouob/seckill_service/service/api-gateway/internal/client"
	"github.com/peterouob/seckill_service/service/api-gateway/internal/router"
	"go.uber.org/fx"
)

const serviceName = "api-gateway"

func main() {
	_ = godotenv.Load()
	logger.InitLogger(serviceName)

	fx.New(
		fx.Provide(func() (*config.Config, error) { return config.Load(serviceName, config.SectionJWT) }),
		auth.Module,
		transport.HTTPServerModule,
		client.Module,
		router.Module,
	).Run()
}
