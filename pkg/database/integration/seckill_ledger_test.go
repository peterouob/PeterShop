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

func insertBarrier(db *sql.DB, gid int64, branchID, op, onConflict string) (sql.Result, error) {
	return db.ExecContext(context.Background(),
		`INSERT INTO tcc_barrier (gid, branch_id, op, created_at) VALUES ($1, $2, $3, now()) `+onConflict,
		gid, branchID, op)
}

func requireAffected(t *testing.T, res sql.Result, want int64) {
	t.Helper()
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, want, n)
}

func TestSeckillSchema_LedgerOneRowPerOrder(t *testing.T) {
	db := seckillDB(t)
	_, err := insertLedger(db, 1, ledgerLocked, "")
	require.NoError(t, err)

	_, err = insertLedger(db, 1, ledgerLocked, "")
	requireViolation(t, err, uniqueViolation, "stock_ledger_pkey")

	res, err := insertLedger(db, 1, ledgerLocked, "ON CONFLICT (order_no) DO NOTHING")
	require.NoError(t, err, "redelivery must not be an error, or the whole Kafka batch is redelivered")
	requireAffected(t, res, 0)
}

func TestSeckillSchema_LedgerStatus(t *testing.T) {
	db := seckillDB(t)
	for i, s := range []int16{ledgerLocked, ledgerSold, ledgerReleased} {
		_, err := insertLedger(db, int64(10+i), s, "")
		require.NoError(t, err, "status=%d must be accepted", s)
	}
	for i, s := range []int16{0, 4} {
		_, err := insertLedger(db, int64(20+i), s, "")
		requireViolation(t, err, checkViolation, "ck_ledger_status")
	}
}

func TestSeckillSchema_BarrierUniqueTriple(t *testing.T) {
	db := seckillDB(t)
	_, err := insertBarrier(db, 1, "stock", "try", "")
	require.NoError(t, err)

	_, err = insertBarrier(db, 1, "stock", "try", "")
	requireViolation(t, err, uniqueViolation, "uk_barrier")

	_, err = insertBarrier(db, 1, "stock", "cancel", "")
	require.NoError(t, err, "cancel on the same branch is a different op and must be accepted")
}

func TestSeckillSchema_BarrierRejectsBadOp(t *testing.T) {
	db := seckillDB(t)
	_, err := insertBarrier(db, 1, "stock", "undo", "")
	requireViolation(t, err, checkViolation, "ck_barrier_op")
}

func TestSeckillSchema_BarrierOnConflictSilent(t *testing.T) {
	db := seckillDB(t)
	res, err := insertBarrier(db, 1, "stock", "try", "ON CONFLICT DO NOTHING")
	require.NoError(t, err)
	requireAffected(t, res, 1)

	res, err = insertBarrier(db, 1, "stock", "try", "ON CONFLICT DO NOTHING")
	require.NoError(t, err)
	requireAffected(t, res, 0)
}

func TestSeckillSchema_HotInsertTablesHaveNoForeignKey(t *testing.T) {
	db := seckillDB(t)
	for _, table := range []string{"stock_ledger", "tcc_barrier"} {
		var n int
		require.NoError(t, db.QueryRowContext(context.Background(),
			`SELECT count(*) FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'f'`, table).Scan(&n))
		require.Zero(t, n, "%s must not have a foreign key", table)
	}
}
