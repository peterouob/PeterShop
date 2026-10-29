package database

import (
	"context"
	"fmt"

	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/logger"
	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func open(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.Postgres.DSN()), &gorm.Config{
		Logger:                 gormlogger.Default.LogMode(gormlogger.Warn),
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: open %s:%d/%s: %w", cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.Database, err)
	}

	return db, nil
}

var Module = fx.Module("postgres",
	fx.Provide(func(lc fx.Lifecycle, cfg *config.Config) (*gorm.DB, error) {
		db, err := open(cfg)
		if err != nil {
			return nil, err
		}

		sql, err := db.DB()
		if err != nil {
			return nil, err
		}

		lc.Append(fx.Hook{
			OnStart: func(context.Context) error {

				sql.SetMaxOpenConns(cfg.Postgres.MaxOpenConns)
				sql.SetMaxIdleConns(cfg.Postgres.MaxIdleConns)
				sql.SetConnMaxLifetime(cfg.Postgres.ConnMaxLifetime)

				if err := sql.Ping(); err != nil {
					return err
				}
				logger.Log("postgres: connect")
				return nil
			},
			OnStop: func(context.Context) error {
				logger.Log("postgres: closing connection pool")
				return sql.Close()
			},
		})
		return db, nil
	}),
)
