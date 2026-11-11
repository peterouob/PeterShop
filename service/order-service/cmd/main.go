package main

import (
	"context"

	"github.com/joho/godotenv"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/database"
	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/pkg/logger"
	"github.com/peterouob/seckill_service/pkg/mq"
	"github.com/peterouob/seckill_service/pkg/mq/kafka"
	"github.com/peterouob/seckill_service/service/order-service/internal/consumer"
	"github.com/peterouob/seckill_service/service/order-service/internal/repository"
	"github.com/peterouob/seckill_service/service/order-service/internal/service"
	"go.uber.org/fx"
)

const (
	serviceName   = "order-service"
	consumerGroup = "order-create"
)

var orderModule = fx.Module(serviceName,
	fx.Provide(
		repository.NewOrderRepo,
		service.NewOrderService,
		consumer.NewHandler,
	),
	fx.Invoke(runConsumer),
)

func runConsumer(lc fx.Lifecycle, c mq.Consumer, h *consumer.Handler) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				defer close(done)
				if err := c.Start(ctx, []string{event.TopicOrderReserved}, h.Handle); err != nil {
					logger.Errorf(err, "order-create consumer stopped")
				}
			}()
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			cancel()
			select {
			case <-done:
			case <-stopCtx.Done():
			}
			return nil
		},
	})
}

var app = fx.Options(
	fx.Provide(func() (*config.Config, error) {
		return config.Load(serviceName, config.SectionPostgres, config.SectionKafka)
	}),
	database.PoolModule,
	kafka.ProducerModule,
	kafka.ConsumerModule(consumerGroup),
	orderModule,
)

func main() {
	_ = godotenv.Load()
	logger.InitLogger(serviceName)

	fx.New(app).Run()
}
