package database

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/logger"
	"go.uber.org/fx"
)

func newPoolConfig(cfg *config.Config) (*pgxpool.Config, error) {
	p := cfg.Postgres

	pc, err := pgxpool.ParseConfig(p.DSN())
	if err != nil {
		return nil, fmt.Errorf("pgxpool: parse config for %s:%d/%s: %w", p.Host, p.Port, p.Database, err)
	}

	// config.Load guarantees 0 <= MinConns <= MaxOpenConns <= MaxInt32 and MaxOpenConns >= 1.
	pc.MaxConns = int32(p.MaxOpenConns)
	pc.MinConns = int32(p.MinConns)

	if p.ConnMaxLifetime > 0 {
		pc.MaxConnLifetime = p.ConnMaxLifetime
		pc.MaxConnLifetimeJitter = p.ConnMaxLifetime / 10
	}
	if p.ConnMaxIdleTime > 0 {
		pc.MaxConnIdleTime = p.ConnMaxIdleTime
	}

	pc.ConnConfig.ConnectTimeout = p.ConnectTimeout

	if _, ok := pc.ConnConfig.RuntimeParams["application_name"]; !ok {
		pc.ConnConfig.RuntimeParams["application_name"] = "seckill_service"
	}

	if p.StatementTimeout > 0 {
		pc.ConnConfig.RuntimeParams["statement_timeout"] =
			strconv.FormatInt(p.StatementTimeout.Milliseconds(), 10)
	}

	return pc, nil
}

var PoolModule = fx.Module("pgxpool",
	fx.Provide(func(lc fx.Lifecycle, cfg *config.Config) (*pgxpool.Pool, error) {
		pc, err := newPoolConfig(cfg)
		if err != nil {
			return nil, err
		}

		pool, err := pgxpool.NewWithConfig(context.Background(), pc)
		if err != nil {
			return nil, fmt.Errorf("pgxpool: create pool: %w", err)
		}

		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				if err := pingWithRetry(ctx, pool.Ping, 3, 500*time.Millisecond); err != nil {
					pool.Close()
					return fmt.Errorf("pgxpool: ping: %w", err)
				}

				logger.Log("pgxpool: connected")
				return nil
			},
			OnStop: func(ctx context.Context) error {
				logger.Log("pgxpool: closing")
				done := make(chan struct{})
				go func() {
					pool.Close()
					close(done)
				}()
				select {
				case <-done:
					return nil
				case <-ctx.Done():
					return fmt.Errorf("pgxpool: close timed out: %w", ctx.Err())
				}
			},
		})
		return pool, nil
	}),
)

func pingWithRetry(ctx context.Context, ping func(context.Context) error, attempts int, wait time.Duration) error {
	var err error
	for i := range attempts {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%w (last ping error: %v)", ctx.Err(), err)
			case <-time.After(wait):
			}
		}
		if err = ping(ctx); err == nil {
			return nil
		}
		logger.Logf("pgxpool: ping attempt %d/%d failed: %v", i+1, attempts, err)
	}
	return err
}
