//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var stale = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func TestUpdatedAt_TriggerRefreshesOnUpdate(t *testing.T) {
	cases := []struct {
		dir, table, insert, update string
	}{
		{"user", "users",
			`INSERT INTO users (user_id, username, password, created_at, updated_at) VALUES ('u1', 'n1', 'p', now(), $1)`,
			`UPDATE users SET username = 'n2'`},
		{"seckill", "seckill_activity",
			`INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
			 VALUES (1, 'p1', 100, now(), now() + interval '1 hour', 0, now(), $1)`,
			`UPDATE seckill_activity SET status = 1`},
		{"seckill", "stock_ledger",
			`INSERT INTO stock_ledger (order_no, activity_id, user_id, status, created_at, updated_at) VALUES (1, 1, 'u1', 1, now(), $1)`,
			`UPDATE stock_ledger SET status = 2`},
		{"order", "orders",
			`INSERT INTO orders (order_no, user_id, product_id, activity_id, amount, status, expire_at, created_at, updated_at)
			 VALUES (1, 'u1', 'p1', 1, 100, 1, now(), now(), $1)`,
			`UPDATE orders SET status = 2`},
		{"order", "tcc_transaction",
			`INSERT INTO tcc_transaction (gid, business, order_no, amount, status, next_retry_at, created_at, updated_at)
			 VALUES (1, 'order_pay', 1, 100, 1, now(), now(), $1)`,
			`UPDATE tcc_transaction SET status = 2`},
		{"order", "tcc_branch",
			`INSERT INTO tcc_branch (gid, branch_id, action, status, created_at, updated_at) VALUES (1, 'payment', 'try', 0, now(), $1)`,
			`UPDATE tcc_branch SET status = 1`},
		{"payment", "payment_freeze",
			`INSERT INTO payment_freeze (gid, order_no, user_id, amount, status, created_at, updated_at) VALUES (1, 1, 'u1', 100, 1, now(), $1)`,
			`UPDATE payment_freeze SET status = 2`},
	}
	for _, tc := range cases {
		t.Run(tc.dir+"/"+tc.table, func(t *testing.T) {
			db := migratedDB(t, tc.dir)
			ctx := context.Background()
			_, err := db.ExecContext(ctx, tc.insert, stale)
			require.NoError(t, err)

			_, err = db.ExecContext(ctx, tc.update)
			require.NoError(t, err)

			var got time.Time
			require.NoError(t, db.QueryRowContext(ctx, `SELECT updated_at FROM `+tc.table).Scan(&got))
			require.True(t, got.After(stale.AddDate(1, 0, 0)), "updated_at must be refreshed by the trigger, got %s", got)
		})
	}
}

func TestUpdatedAt_EveryTableWithTheColumnHasTheTrigger(t *testing.T) {
	for _, dir := range []string{"user", "seckill", "order", "payment"} {
		t.Run(dir, func(t *testing.T) {
			rows, err := migratedDB(t, dir).QueryContext(context.Background(), `
SELECT c.table_name FROM information_schema.columns c
 WHERE c.table_schema = 'public' AND c.column_name = 'updated_at'
   AND NOT EXISTS (
     SELECT 1 FROM pg_trigger tg
      WHERE tg.tgrelid = quote_ident(c.table_name)::regclass
        AND NOT tg.tgisinternal
        AND tg.tgfoid = 'set_updated_at'::regproc)`)
			require.NoError(t, err)
			defer rows.Close()
			var missing []string
			for rows.Next() {
				var name string
				require.NoError(t, rows.Scan(&name))
				missing = append(missing, name)
			}
			require.NoError(t, rows.Err())
			require.Empty(t, missing, "new tables with updated_at must get the trigger (D014)")
		})
	}
}

func TestUpdatedAt_HotRowHasNoUserTrigger(t *testing.T) {
	var n int
	require.NoError(t, migratedDB(t, "seckill").QueryRowContext(context.Background(),
		`SELECT count(*) FROM pg_trigger WHERE tgrelid = 'activity_stock'::regclass AND NOT tgisinternal`).Scan(&n))
	require.Zero(t, n, "activity_stock is the hot row of every winner tx; no triggers (D014)")
}
