//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

const invalidTextRepresentation = "22P02"

func TestSeckillSchema_LedgerOneWinPerUserPerActivity(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedSlots(t, db, 1, 4)
	seedSlots(t, db, 2, 3)
	_, err := insertLedgerRow(db, ledgerRow{orderNo: 1, slotNo: 1, activityID: 1, userID: "u1", status: ledgerLocked}, "")
	require.NoError(t, err)

	_, err = insertLedgerRow(db, ledgerRow{orderNo: 2, slotNo: 2, activityID: 1, userID: "u1", status: ledgerLocked}, "")
	requireViolation(t, err, uniqueViolation, "uk_ledger_activity_user")

	_, err = insertLedgerRow(db, ledgerRow{orderNo: 3, slotNo: 3, activityID: 2, userID: "u1", status: ledgerLocked}, "")
	require.NoError(t, err, "same user in the next activity")

	_, err = insertLedgerRow(db, ledgerRow{orderNo: 4, slotNo: 4, activityID: 1, userID: "u2", status: ledgerLocked}, "")
	require.NoError(t, err, "another user in the same activity")
}

func TestSeckillSchema_LedgerRetryVsSecondWin(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedSlots(t, db, 1, 2)
	_, err := insertLedgerRow(db, ledgerRow{orderNo: 1, slotNo: 1, activityID: 1, userID: "u1", status: ledgerLocked}, "")
	require.NoError(t, err)

	res, err := insertLedgerRow(db, ledgerRow{orderNo: 1, slotNo: 1, activityID: 1, userID: "u1", status: ledgerLocked},
		"ON CONFLICT (order_no) DO NOTHING")
	require.NoError(t, err, "a retry of the same order_no is silent")
	requireAffected(t, res, 0)

	_, err = insertLedgerRow(db, ledgerRow{orderNo: 2, slotNo: 2, activityID: 1, userID: "u1", status: ledgerLocked},
		"ON CONFLICT (order_no) DO NOTHING")
	requireViolation(t, err, uniqueViolation, "uk_ledger_activity_user")
}

func insertOutbox(db *sql.DB, orderNo int64, payload string) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO outbox (order_no, payload) VALUES ($1, $2)`, orderNo, payload)
	return err
}

func TestSeckillSchema_OutboxOneRowPerOrder(t *testing.T) {
	db := migratedDB(t, "seckill")
	require.NoError(t, insertOutbox(db, 1, `{"order_no":1}`))
	requireViolation(t, insertOutbox(db, 1, `{"order_no":1}`), uniqueViolation, "uk_outbox_order_no")
}

func TestSeckillSchema_OutboxPayloadMustBeJSON(t *testing.T) {
	db := migratedDB(t, "seckill")
	err := insertOutbox(db, 1, "not json")

	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "want PgError, got %v", err)
	require.Equal(t, invalidTextRepresentation, pgErr.Code, pgErr.Message)
}
