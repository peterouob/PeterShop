//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	freezeFrozen    = 1
	freezeConfirmed = 2
	freezeReleased  = 3

	flowDebit  = 1
	flowRefund = 2
)

func insertAccount(db *sql.DB, userID string, available, frozen int64) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO account (user_id, available, frozen) VALUES ($1,$2,$3)`, userID, available, frozen)
	return err
}

type freezeRow struct {
	gid     int64
	orderNo int64
	amount  int64
	status  int16
}

func validFreeze(gid int64) freezeRow {
	return freezeRow{gid: gid, orderNo: 1, amount: 100, status: freezeFrozen}
}

func insertFreeze(db *sql.DB, r freezeRow) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO payment_freeze (gid, order_no, user_id, amount, status, created_at, updated_at)
		 VALUES ($1,$2,'u1',$3,$4,now(),now())`,
		r.gid, r.orderNo, r.amount, r.status)
	return err
}

type flowRow struct {
	flowNo    int64
	orderNo   int64
	amount    int64
	direction int16
}

func validFlow(flowNo int64) flowRow {
	return flowRow{flowNo: flowNo, orderNo: flowNo, amount: 100, direction: flowDebit}
}

func insertFlow(db *sql.DB, r flowRow) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO payment_flow (flow_no, gid, order_no, user_id, amount, direction, created_at)
		 VALUES ($1,1,$2,'u1',$3,$4,now())`,
		r.flowNo, r.orderNo, r.amount, r.direction)
	return err
}

func TestPaymentSchema_AccountOnePerUser(t *testing.T) {
	db := migratedDB(t, "payment")
	require.NoError(t, insertAccount(db, "u1", 100, 0))
	requireViolation(t, insertAccount(db, "u1", 100, 0), uniqueViolation, "uk_user")
}

func TestPaymentSchema_AccountRejectsNegative(t *testing.T) {
	db := migratedDB(t, "payment")
	cases := []struct {
		name              string
		available, frozen int64
	}{
		{"negative available", -1, 0},
		{"negative frozen", 0, -1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := insertAccount(db, tc.name+string(rune('0'+i)), tc.available, tc.frozen)
			requireViolation(t, err, checkViolation, "ck_account_non_negative")
		})
	}
}

// pay_try relies on this instead of a version column: overdrawing must fail inside the UPDATE.
func TestPaymentSchema_AccountOverdrawUpdateRejected(t *testing.T) {
	db := migratedDB(t, "payment")
	require.NoError(t, insertAccount(db, "u1", 100, 0))

	_, err := db.ExecContext(context.Background(),
		`UPDATE account SET available = available - 200, frozen = frozen + 200 WHERE user_id = 'u1'`)
	requireViolation(t, err, checkViolation, "ck_account_non_negative")
}

func TestPaymentSchema_FreezeOnePerGid(t *testing.T) {
	db := migratedDB(t, "payment")
	require.NoError(t, insertFreeze(db, validFreeze(1)))
	requireViolation(t, insertFreeze(db, validFreeze(1)), uniqueViolation, "payment_freeze_pkey")

	retry := validFreeze(2)
	require.NoError(t, insertFreeze(db, retry), "a retried payment for the same order gets a new gid and must be accepted")
}

func TestPaymentSchema_FreezeAcceptsEveryStatus(t *testing.T) {
	db := migratedDB(t, "payment")
	for _, s := range []int16{freezeFrozen, freezeConfirmed, freezeReleased} {
		r := validFreeze(int64(100 + s))
		r.status = s
		require.NoError(t, insertFreeze(db, r), "status=%d", s)
	}
}

func TestPaymentSchema_FreezeRejects(t *testing.T) {
	db := migratedDB(t, "payment")
	cases := []struct {
		name       string
		mutate     func(*freezeRow)
		constraint string
	}{
		{"status out of 1~3", func(r *freezeRow) { r.status = 4 }, "ck_freeze_status"},
		{"zero amount", func(r *freezeRow) { r.amount = 0 }, "ck_freeze_amount"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validFreeze(int64(200 + i))
			tc.mutate(&r)
			requireViolation(t, insertFreeze(db, r), checkViolation, tc.constraint)
		})
	}
}

func TestPaymentSchema_FlowNoUnique(t *testing.T) {
	db := migratedDB(t, "payment")
	require.NoError(t, insertFlow(db, validFlow(1)))

	dup := validFlow(1)
	dup.orderNo = 99
	requireViolation(t, insertFlow(db, dup), uniqueViolation, "uk_flow")
}

func TestPaymentSchema_OneDebitOneRefundPerOrder(t *testing.T) {
	db := migratedDB(t, "payment")
	require.NoError(t, insertFlow(db, flowRow{flowNo: 1, orderNo: 7, amount: 100, direction: flowDebit}))

	requireViolation(t, insertFlow(db, flowRow{flowNo: 2, orderNo: 7, amount: 100, direction: flowDebit}),
		uniqueViolation, "uk_order_direction")

	require.NoError(t, insertFlow(db, flowRow{flowNo: 3, orderNo: 7, amount: 100, direction: flowRefund}))
	requireViolation(t, insertFlow(db, flowRow{flowNo: 4, orderNo: 7, amount: 100, direction: flowRefund}),
		uniqueViolation, "uk_order_direction")
}

func TestPaymentSchema_FlowRejects(t *testing.T) {
	db := migratedDB(t, "payment")
	cases := []struct {
		name       string
		mutate     func(*flowRow)
		constraint string
	}{
		{"direction out of 1~2", func(r *flowRow) { r.direction = 3 }, "ck_flow_direction"},
		{"zero amount", func(r *flowRow) { r.amount = 0 }, "ck_flow_amount"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validFlow(int64(200 + i))
			tc.mutate(&r)
			requireViolation(t, insertFlow(db, r), checkViolation, tc.constraint)
		})
	}
}
