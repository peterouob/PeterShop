package integration

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/stretchr/testify/require"
)

const (
	foreignKeyViolation = "23503"
	uniqueViolation     = "23505"
	checkViolation      = "23514"
)

func seckillDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testkit.OpenContainer(t, "seckill")
	require.NoError(t, db.Migrator(t).Up())
	return db.Open(t)
}

func requireViolation(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "want PgError, got %v", err)
	require.Equal(t, code, pgErr.Code, pgErr.Message)
	require.Equal(t, constraint, pgErr.ConstraintName, pgErr.Message)
}
