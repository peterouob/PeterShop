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
	"github.com/peterouob/seckill_service/pkg/cache"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/database"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/repository"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
)

func main() {
	activityID := flag.Int64("activity", 0, "activity_id whose Redis state is rebuilt from db_seckill")
	flag.Parse()
	_ = godotenv.Load()

	if *activityID <= 0 {
		slog.Info("usage: rebuild --activity <id>")
		os.Exit(2)
	}
	if err := run(*activityID); err != nil {
		slog.Error("rebuild failed, safe to rerun", "err", err)
		os.Exit(1)
	}
}

func run(activityID int64) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		pool *pgxpool.Pool
		rdb  *redis.Client
	)
	app := fx.New(
		fx.NopLogger,
		fx.Provide(func() (*config.Config, error) {
			return config.Load("seckill-rebuild", config.SectionPostgres, config.SectionRedis)
		}),
		database.PoolModule,
		cache.Module,
		fx.Populate(&pool, &rdb),
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

	if err := repository.NewRebuilder(rdb, pool).Rebuild(ctx, activityID); err != nil {
		return err
	}
	fmt.Printf("activity=%d rebuilt\n", activityID)
	return nil
}
