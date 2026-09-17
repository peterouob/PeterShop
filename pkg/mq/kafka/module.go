package kafka

import (
	"context"

	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/mq"
	"go.uber.org/fx"
)

var ProducerModule = fx.Module("kafka-producer",
	fx.Provide(func(lc fx.Lifecycle, cfg *config.Config) (mq.Producer, error) {
		p, err := NewProducer(cfg)
		if err != nil {
			return nil, err
		}
		lc.Append(fx.Hook{
			OnStop: func(context.Context) error { return p.Close() },
		})
		return p, nil
	}),
)

func ConsumerModule(groupID string) fx.Option {
	return fx.Module("kafka-consumer",
		fx.Provide(func(lc fx.Lifecycle, cfg *config.Config) (mq.Consumer, error) {
			c, err := NewConsumer(cfg, groupID)
			if err != nil {
				return nil, err
			}
			lc.Append(fx.Hook{
				OnStop: func(context.Context) error { return c.Close() },
			})
			return c, nil
		}),
	)
}
