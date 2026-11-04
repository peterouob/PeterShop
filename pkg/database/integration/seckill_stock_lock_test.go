//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const noDataFound = "P0002"

func stockLock(db *sql.DB, orderNo, activityID int64, userID string) (bool, error) {
	var applied bool
	err := db.QueryRowContext(context.Background(),
		`SELECT stock_lock($1, $2, $3)`, orderNo, activityID, userID).Scan(&applied)
	return applied, err
}

func readStock(t *testing.T, db *sql.DB, activityID int64) stockRow {
	t.Helper()
	r := stockRow{activityID: activityID}
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT total, available, locked, sold FROM activity_stock WHERE activity_id = $1`, activityID).
		Scan(&r.total, &r.available, &r.locked, &r.sold))
	return r
}

func countLedger(t *testing.T, db *sql.DB, activityID int64) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM stock_ledger WHERE activity_id = $1`, activityID).Scan(&n))
	return n
}

func seedStock(t *testing.T, db *sql.DB, r stockRow) {
	t.Helper()
	seedActivities(t, db, r.activityID)
	require.NoError(t, insertStock(db, r))
}

func TestStockLock_FirstAndReply(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedStock(t, db, stockRow{activityID: 1, total: 10, available: 10})

	applied, err := stockLock(db, 100, 1, "u1")

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, 1, countLedger(t, db, 1))
	stock := readStock(t, db, 1)
	assert.Equal(t, 9, stock.available)
	assert.Equal(t, 1, stock.locked)

	applied, err = stockLock(db, 100, 1, "u1")
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, 1, countLedger(t, db, 1))
	stock = readStock(t, db, 1)
	assert.Equal(t, 9, stock.available)
	assert.Equal(t, 1, stock.locked)
}

func TestStockLock_SameUserNewOrder(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedStock(t, db, stockRow{activityID: 1, total: 10, available: 10})
	_, err := stockLock(db, 100, 1, "u1")
	require.NoError(t, err)

	_, err = stockLock(db, 101, 1, "u1")

	requireViolation(t, err, uniqueViolation, "uk_ledger_activity_user")
	require.Equal(t, 1, countLedger(t, db, 1))
	stock := readStock(t, db, 1)
	assert.Equal(t, 9, stock.available)
	assert.Equal(t, 1, stock.locked)
}

func TestStockLock_SoldOut(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedStock(t, db, stockRow{activityID: 1, total: 10, available: 0, sold: 10})

	_, err := stockLock(db, 100, 1, "u1")

	requireViolation(t, err, checkViolation, "ck_stock_non_negative")
	require.Equal(t, 0, countLedger(t, db, 1))
	stock := readStock(t, db, 1)
	assert.Equal(t, 0, stock.available)
	assert.Equal(t, 0, stock.locked)
	assert.Equal(t, 10, stock.sold)
}

func TestStockLock_NoStockRow(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedActivities(t, db, 1)

	_, err := stockLock(db, 100, 1, "u1")

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, noDataFound, pgErr.Code)
	require.Equal(t, 0, countLedger(t, db, 1))
}
