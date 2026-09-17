package database

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/peterouob/seckill_service/pkg/config"
)

type Direction string

const (
	Up   Direction = "up"
	Down Direction = "down"
)

var ErrUnknownDirection = errors.New("migrate: direction must be \"up\" or \"down\"")

func Migrate(cfg *config.Config, sourceURL string, dir Direction, steps int) error {
	m, err := migrate.New(sourceURL, cfg.MySQL.URL())
	if err != nil {
		return fmt.Errorf("migrate: open source %s: %w", sourceURL, err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		_, _ = srcErr, dbErr
	}()

	if err := apply(m, dir, steps); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return nil
		}
		return fmt.Errorf("migrate: apply %s: %w", dir, err)
	}
	return nil
}

func apply(m *migrate.Migrate, dir Direction, steps int) error {
	switch {
	case dir == Up && steps <= 0:
		return m.Up()
	case dir == Up:
		return m.Steps(steps)
	case dir == Down && steps <= 0:
		return m.Down()
	case dir == Down:
		return m.Steps(-steps)
	default:
		return ErrUnknownDirection
	}
}

func Version(cfg *config.Config, sourceURL string) (uint, bool, error) {
	m, err := migrate.New(sourceURL, cfg.MySQL.URL())
	if err != nil {
		return 0, false, fmt.Errorf("migrate: open source %s: %w", sourceURL, err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		_, _ = srcErr, dbErr
	}()

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, fmt.Errorf("migrate: read version: %w", err)
	}
	return version, dirty, nil
}
