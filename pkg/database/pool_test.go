package database

import (
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
