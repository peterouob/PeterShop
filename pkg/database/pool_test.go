package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPoolConfig_SmallCapNotRaised(t *testing.T) {
	cfg := &config.Config{Postgres: config.Postgres{
		Host: "localhost", Port: 5432, User: "u", Password: "p", Database: "db",
		MaxOpenConns:   2,
		MinConns:       2,
		ConnectTimeout: time.Second,
	}}

	pc, err := newPoolConfig(cfg)
	require.NoError(t, err)
	assert.EqualValues(t, 2, pc.MaxConns, "the configured cap must not be raised (e.g. to NumCPU)")
	assert.EqualValues(t, 2, pc.MinConns)
}

func TestPingWithRetry(t *testing.T) {
	errDown := errors.New("db down")
	failUntil := func(okOn int, calls *int) func(context.Context) error {
		return func(context.Context) error {
			*calls++
			if *calls >= okOn {
				return nil
			}
			return errDown
		}
	}

	t.Run("succeeds after retries", func(t *testing.T) {
		calls := 0
		require.NoError(t, pingWithRetry(t.Context(), failUntil(2, &calls), 3, time.Millisecond))
		assert.Equal(t, 2, calls)
	})

	t.Run("gives up after the last attempt without an extra wait", func(t *testing.T) {
		calls := 0
		start := time.Now()
		err := pingWithRetry(t.Context(), failUntil(99, &calls), 3, 50*time.Millisecond)
		require.ErrorIs(t, err, errDown)
		assert.Equal(t, 3, calls)
		assert.Less(t, time.Since(start), 140*time.Millisecond, "3 attempts need only 2 waits")
	})

	t.Run("stops waiting when ctx is done", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		ping := func(context.Context) error { calls++; cancel(); return errDown }
		start := time.Now()
		err := pingWithRetry(ctx, ping, 3, time.Hour)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 1, calls)
		assert.Less(t, time.Since(start), time.Second)
	})
}
