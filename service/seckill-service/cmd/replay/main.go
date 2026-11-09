package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/database"
	"github.com/peterouob/seckill_service/pkg/mq"
	"github.com/peterouob/seckill_service/pkg/mq/kafka"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository/postgresql"
	"go.uber.org/fx"
)

const batchSize = 500

func main() {
	activityID := flag.Int64("activity", 0, "activity_id whose orders are replayed")
	flag.Parse()
	_ = godotenv.Load()

	if *activityID <= 0 {
		slog.Info("usage: replay --activity <id>")
		os.Exit(2)
	}
	if err := run(*activityID); err != nil {
		slog.Error("replay failed, safe to rerun", "err", err)
		os.Exit(1)
	}
}

func run(activityID int64) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		pool     *pgxpool.Pool
		producer mq.Producer
	)
	app := fx.New(
		fx.NopLogger,
		fx.Provide(func() (*config.Config, error) {
			return config.Load("seckill-replay", config.SectionPostgres, config.SectionKafka)
		}),
		database.PoolModule,
		kafka.ProducerModule,
		fx.Populate(&pool, &producer),
	)
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	defer func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if err := app.Stop(stopCtx); err != nil {
			slog.Error("stop failed", "err", err)
		}
	}()

	sent, err := postgresql.ReplayOrders(ctx, pool, producer, activityID, batchSize)
	fmt.Printf("activity=%d sent=%d\n", activityID, sent)
	return err
}
