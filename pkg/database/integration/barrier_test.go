//go:build integration

package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var barrierDirs = []string{"seckill", "payment"}

func insertBarrier(db *sql.DB, gid int64, branchID, op, onConflict string) (sql.Result, error) {
	return db.ExecContext(context.Background(),
		`INSERT INTO tcc_barrier (gid, branch_id, op, created_at) VALUES ($1, $2, $3, now()) `+onConflict,
		gid, branchID, op)
}

func TestBarrier_UniqueTriple(t *testing.T) {
	for _, dir := range barrierDirs {
		t.Run(dir, func(t *testing.T) {
			db := migratedDB(t, dir)
			_, err := insertBarrier(db, 1, dir, "try", "")
			require.NoError(t, err)

			_, err = insertBarrier(db, 1, dir, "try", "")
			requireViolation(t, err, uniqueViolation, "uk_barrier")

			_, err = insertBarrier(db, 1, dir, "cancel", "")
			require.NoError(t, err, "cancel on the same branch is a different op and must be accepted")
		})
	}
}

func TestBarrier_RejectsBadOp(t *testing.T) {
	for _, dir := range barrierDirs {
		t.Run(dir, func(t *testing.T) {
			_, err := insertBarrier(migratedDB(t, dir), 1, dir, "undo", "")
			requireViolation(t, err, checkViolation, "ck_barrier_op")
		})
	}
}

func TestBarrier_OnConflictSilent(t *testing.T) {
	for _, dir := range barrierDirs {
		t.Run(dir, func(t *testing.T) {
			db := migratedDB(t, dir)
			res, err := insertBarrier(db, 1, dir, "try", "ON CONFLICT DO NOTHING")
			require.NoError(t, err)
			requireAffected(t, res, 1)

			res, err = insertBarrier(db, 1, dir, "try", "ON CONFLICT DO NOTHING")
			require.NoError(t, err)
			requireAffected(t, res, 0)
		})
	}
}

func TestBarrier_BranchIDHoldsWhatCoordinatorHolds(t *testing.T) {
	for _, dir := range barrierDirs {
		t.Run(dir, func(t *testing.T) {
			_, err := insertBarrier(migratedDB(t, dir), 1, strings.Repeat("b", 64), "try", "")
			require.NoError(t, err, "order tcc_branch.branch_id is varchar(64); participants must accept the same")
		})
	}
}

func TestHotInsertTablesHaveNoForeignKey(t *testing.T) {
	cases := map[string][]string{
		"seckill": {"stock_ledger", "tcc_barrier", "outbox"},
		"order":   {"orders", "tcc_transaction", "tcc_branch"},
		"payment": {"payment_freeze", "payment_flow", "tcc_barrier"},
	}
	for dir, tables := range cases {
		t.Run(dir, func(t *testing.T) {
			db := migratedDB(t, dir)
			for _, table := range tables {
				var n int
				require.NoError(t, db.QueryRowContext(context.Background(),
					`SELECT count(*) FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'f'`, table).Scan(&n))
				require.Zero(t, n, "%s must not have a foreign key", table)
			}
		})
	}
}
