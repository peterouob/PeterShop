//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	ledgerLocked   = 1
	ledgerSold     = 2
	ledgerReleased = 3
)

func insertLedger(db *sql.DB, orderNo int64, status int16, onConflict string) (sql.Result, error) {
	return db.ExecContext(context.Background(),
		`INSERT INTO stock_ledger (order_no, activity_id, user_id, status, created_at, updated_at)
		 VALUES ($1, 1, 'u1', $2, now(), now()) `+onConflict,
		orderNo, status)
}

func requireAffected(t *testing.T, res sql.Result, want int64) {
	t.Helper()
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, want, n)
}

func TestSeckillSchema_LedgerOneRowPerOrder(t *testing.T) {
	db := migratedDB(t, "seckill")
	_, err := insertLedger(db, 1, ledgerLocked, "")
	require.NoError(t, err)

	_, err = insertLedger(db, 1, ledgerLocked, "")
	requireViolation(t, err, uniqueViolation, "stock_ledger_pkey")

	res, err := insertLedger(db, 1, ledgerLocked, "ON CONFLICT (order_no) DO NOTHING")
	require.NoError(t, err, "redelivery must not be an error, or the whole Kafka batch is redelivered")
	requireAffected(t, res, 0)
}

func TestSeckillSchema_LedgerStatus(t *testing.T) {
	db := migratedDB(t, "seckill")
	for i, s := range []int16{ledgerLocked, ledgerSold, ledgerReleased} {
		_, err := insertLedger(db, int64(10+i), s, "")
		require.NoError(t, err, "status=%d must be accepted", s)
	}
	for i, s := range []int16{0, 4} {
		_, err := insertLedger(db, int64(20+i), s, "")
		requireViolation(t, err, checkViolation, "ck_ledger_status")
	}
}
