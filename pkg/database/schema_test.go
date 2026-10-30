package database

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/peterouob/seckill_service/pkg/testkit"
	"github.com/stretchr/testify/require"
)

func TestMigrations_UpDownUp(t *testing.T) {
	tests := []struct {
		dir  string
		skip string
	}{
		{dir: "user"},
		{dir: "seckill", skip: "1"},
		{dir: "order", skip: "1"},
		{dir: "payment", skip: "1"},
	}

	for _, tc := range tests {
		t.Run(tc.dir, func(t *testing.T) {
			if tc.skip != "" {
				t.Skip(tc.skip)
			}
			db := testkit.OpenContainer(t, tc.dir)
			m := db.Migrator(t)
			conn := db.Open(t)

			require.NoError(t, m.Up(), "up")
			first := schema(t, conn)
			require.NotEmpty(t, first, "up need to create sth...")

			require.NoError(t, m.Down(), "down")
			require.Empty(t, schema(t, conn), "down need to drop anything ...")

			require.NoError(t, m.Up(), "up again")
			require.Equal(t, first, schema(t, conn), "re-up need to be the same with first up")
		})
	}
}

func schema(t *testing.T, db *sql.DB) []string {
	t.Helper()
	const q = `
SELECT 'col ' || table_name || '.' || column_name || ' ' || data_type
  FROM information_schema.columns
 WHERE table_schema = 'public' AND table_name <> 'schema_migrations'
UNION ALL
SELECT 'con ' || conrelid::regclass::text || ' ' || conname || ' ' || pg_get_constraintdef(oid)
  FROM pg_constraint
 WHERE connamespace = 'public'::regnamespace AND conrelid::regclass::text <> 'schema_migrations'
UNION ALL
SELECT 'type ' || typname
  FROM pg_type
 WHERE typnamespace = 'public'::regnamespace AND typtype IN ('e', 'd')
UNION ALL
SELECT 'func ' || proname
  FROM pg_proc
 WHERE pronamespace = 'public'::regnamespace
UNION ALL
SELECT 'idx ' || indexdef
  FROM pg_indexes
 WHERE schemaname = 'public' AND tablename <> 'schema_migrations'
ORDER BY 1`

	rows, err := db.QueryContext(context.Background(), q)
	require.NoError(t, err)
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			panic(fmt.Errorf("failed to close rows: %w", err))
		}
	}(rows)

	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}
