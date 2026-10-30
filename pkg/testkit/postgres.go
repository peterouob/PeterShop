package testkit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	sharedOnce sync.Once
	shared     *TempPostgres
	sharedErr  error
)

type TempPostgresDB struct {
	Name   string
	DSN    string
	dir    string
	server *TempPostgres
}

func (d *TempPostgresDB) url(scheme string) string {
	u := d.server.adminURL
	u.Scheme = scheme
	u.Path = "/" + d.Name
	return u.String()
}

func (d *TempPostgresDB) Migrator(t testing.TB) *migrate.Migrate {
	t.Helper()
	source := "file://" + filepath.ToSlash(filepath.Join(d.server.migrations, d.dir))
	m, err := migrate.New(source, d.url("pgx5"))
	if err != nil {
		t.Fatalf("testkit: migrate.New(%s): %v", source, err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	return m
}

func (d *TempPostgresDB) Open(t testing.TB) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", d.DSN)
	if err != nil {
		t.Fatalf("testkit: open %s: %v", d.Name, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("testkit: ping %s: %v", d.Name, err)
	}
	return db
}

type TempPostgres struct {
	admin      *sql.DB
	adminURL   url.URL
	migrations string
}

func OpenContainer(t testing.TB, dir string) *TempPostgresDB {
	t.Helper()

	sharedOnce.Do(func() {
		shared, sharedErr = newTempPostgres(context.Background())
	})

	if sharedErr != nil {
		t.Fatalf("testkit: failed to connect to postgres: %s", sharedErr.Error())
	}

	return shared.create(t, dir)
}

func (pg *TempPostgres) create(t testing.TB, dir string) *TempPostgresDB {
	t.Helper()
	name := fmt.Sprintf("it_%s_%d", sanitize(dir), time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()

	if _, err := pg.admin.ExecContext(context.Background(), "CREATE DATABASE "+quoted); err != nil {
		t.Fatalf("testkit: create database %s: %v", name, err)
	}

	t.Cleanup(func() {
		if _, err := pg.admin.ExecContext(context.Background(),
			"DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("testkit: drop database %s: %v", name, err)
		}
	})

	d := &TempPostgresDB{Name: name, dir: dir, server: pg}
	d.DSN = d.url("postgres")
	return d
}

func newTempPostgres(ctx context.Context) (_ *TempPostgres, err error) {
	var (
		dsn       string
		container *postgres.PostgresContainer
	)

	dsn, container, err = serverDSN(ctx)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil && container != nil {
			err = errors.Join(err, testcontainers.TerminateContainer(container))
		}
	}()

	var u *url.URL
	u, err = url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	var root string
	root, err = moduleRoot()
	if err != nil {
		return nil, err
	}

	var db *sql.DB
	db, err = sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open admin db: %w", err)
	}
	if err = db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close db after ping error: %w", closeErr))
		}
		return nil, fmt.Errorf("ping admin db: %w", err)
	}

	return &TempPostgres{
		admin:      db,
		adminURL:   *u,
		migrations: filepath.Join(root, "deploy", "migrations"),
	}, nil
}

func serverDSN(ctx context.Context) (string, *postgres.PostgresContainer, error) {
	ctr, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("start postgres container: %w", err),
			testcontainers.TerminateContainer(ctr))
	}
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", nil, errors.Join(err, testcontainers.TerminateContainer(ctr))
	}
	return dsn, ctr, nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("failed to find go.mod")
		}
		dir = parent
	}
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, s)
}
