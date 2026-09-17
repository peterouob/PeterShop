package database

import (
	"context"
	"fmt"

	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/logger"
	"go.uber.org/fx"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func open(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.MySQL.DSN()), &gorm.Config{
		Logger:                 gormlogger.Default.LogMode(gormlogger.Warn),
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
	})
	if err != nil {
		return nil, fmt.Errorf("mysql: open %s:%d/%s: %w", cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.Database, err)
	}

	return db, nil
}

var Module = fx.Module("mysql",
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

				sql.SetMaxOpenConns(cfg.MySQL.MaxOpenConns)
				sql.SetMaxIdleConns(cfg.MySQL.MaxIdleConns)
				sql.SetConnMaxLifetime(cfg.MySQL.ConnMaxLifetime)

				if err := sql.Ping(); err != nil {
					return err
				}
				logger.Log("mysql: connect")
				return nil
			},
			OnStop: func(context.Context) error {
				logger.Log("mysql: closing connection pool")
				return sql.Close()
			},
		})
		return db, nil
	}),
)
