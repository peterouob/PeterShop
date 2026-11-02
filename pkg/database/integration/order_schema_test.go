//go:build integration

package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type orderRow struct {
	orderNo    int64
	userID     string
	productID  string
	activityID int64
	amount     int64
	status     int16
}

func validOrder(orderNo int64) orderRow {
	return orderRow{orderNo: orderNo, userID: "u1", productID: "p1", activityID: 1, amount: 100, status: 1}
}

func insertOrder(db *sql.DB, r orderRow) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO orders (order_no, user_id, product_id, activity_id, amount, status, expire_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,now(),now())`,
		r.orderNo, r.userID, r.productID, r.activityID, r.amount, r.status, time.Now().Add(15*time.Minute))
	return err
}

type tccTxRow struct {
	gid      int64
	business string
	amount   int64
	status   int16
}

func validTccTx(gid int64) tccTxRow {
	return tccTxRow{gid: gid, business: "order_pay", amount: 100, status: 1}
}

func insertTccTx(db *sql.DB, r tccTxRow) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO tcc_transaction (gid, business, order_no, amount, status, next_retry_at, created_at, updated_at)
		 VALUES ($1,$2,1,$3,$4,now(),now(),now())`,
		r.gid, r.business, r.amount, r.status)
	return err
}

type tccBranchRow struct {
	gid      int64
	branchID string
	action   string
	status   int16
}

func validTccBranch(gid int64) tccBranchRow {
	return tccBranchRow{gid: gid, branchID: "payment", action: "try", status: 0}
}

func insertTccBranch(db *sql.DB, r tccBranchRow) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO tcc_branch (gid, branch_id, action, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,now(),now())`,
		r.gid, r.branchID, r.action, r.status)
	return err
}

func TestOrderSchema_D17_OrderNoIsBigint(t *testing.T) {
	db := migratedDB(t, "order")
	var dataType string
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT data_type FROM information_schema.columns
		  WHERE table_schema = 'public' AND table_name = 'orders' AND column_name = 'order_no'`).Scan(&dataType))
	require.Equal(t, "bigint", dataType)

	err := db.QueryRowContext(context.Background(),
		`SELECT data_type FROM information_schema.columns
		  WHERE table_schema = 'public' AND table_name = 'orders' AND column_name = 'order_id'`).Scan(&dataType)
	require.ErrorIs(t, err, sql.ErrNoRows, "order_id must not exist")
}

func TestOrderSchema_OrderNoUnique(t *testing.T) {
	db := migratedDB(t, "order")
	require.NoError(t, insertOrder(db, validOrder(1)))

	dup := validOrder(1)
	dup.userID = "u2"
	requireViolation(t, insertOrder(db, dup), uniqueViolation, "uk_order_no")
}

func TestOrderSchema_OneOrderPerUserActivity(t *testing.T) {
	db := migratedDB(t, "order")
	require.NoError(t, insertOrder(db, validOrder(1)))

	requireViolation(t, insertOrder(db, validOrder(2)), uniqueViolation, "uk_user_activity")

	next := validOrder(3)
	next.activityID = 2
	require.NoError(t, insertOrder(db, next), "same user and product in the next activity must be accepted")
}

func TestOrderSchema_OrderAcceptsEveryStatus(t *testing.T) {
	db := migratedDB(t, "order")
	for status := int16(1); status <= 6; status++ {
		r := validOrder(int64(100 + status))
		r.userID = "u" + string(rune('a'+status))
		r.status = status
		require.NoError(t, insertOrder(db, r), "status=%d", status)
	}
}

func TestOrderSchema_OrderRejects(t *testing.T) {
	db := migratedDB(t, "order")
	cases := []struct {
		name       string
		mutate     func(*orderRow)
		constraint string
	}{
		{"status out of 1~6", func(r *orderRow) { r.status = 7 }, "ck_order_status"},
		{"zero amount", func(r *orderRow) { r.amount = 0 }, "ck_order_amount"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validOrder(int64(200 + i))
			r.userID = tc.name
			tc.mutate(&r)
			requireViolation(t, insertOrder(db, r), checkViolation, tc.constraint)
		})
	}
}

func TestOrderSchema_TccTransactionGidUnique(t *testing.T) {
	db := migratedDB(t, "order")
	require.NoError(t, insertTccTx(db, validTccTx(1)))
	requireViolation(t, insertTccTx(db, validTccTx(1)), uniqueViolation, "uk_gid")
}

func TestOrderSchema_TccTransactionAcceptsEveryStatusAndBusiness(t *testing.T) {
	db := migratedDB(t, "order")
	gid := int64(100)
	for _, business := range []string{"order_pay", "order_refund"} {
		for status := int16(1); status <= 6; status++ {
			gid++
			r := validTccTx(gid)
			r.business, r.status = business, status
			require.NoError(t, insertTccTx(db, r), "business=%s status=%d", business, status)
		}
	}
}

func TestOrderSchema_TccTransactionRejects(t *testing.T) {
	db := migratedDB(t, "order")
	cases := []struct {
		name       string
		mutate     func(*tccTxRow)
		constraint string
	}{
		{"status out of 1~6", func(r *tccTxRow) { r.status = 7 }, "ck_tcc_transaction_status"},
		{"zero amount", func(r *tccTxRow) { r.amount = 0 }, "ck_tcc_transaction_amount"},
		{"unknown business", func(r *tccTxRow) { r.business = "order_pay_typo" }, "ck_tcc_transaction_business"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validTccTx(int64(200 + i))
			tc.mutate(&r)
			requireViolation(t, insertTccTx(db, r), checkViolation, tc.constraint)
		})
	}
}

func TestOrderSchema_TccBranchUniquePerAction(t *testing.T) {
	db := migratedDB(t, "order")
	require.NoError(t, insertTccBranch(db, validTccBranch(1)))
	requireViolation(t, insertTccBranch(db, validTccBranch(1)), uniqueViolation, "uk_branch_action")

	cancel := validTccBranch(1)
	cancel.action = "cancel"
	require.NoError(t, insertTccBranch(db, cancel), "cancel on the same branch is a different action")
}

func TestOrderSchema_TccBranchIDFitsBarrier(t *testing.T) {
	db := migratedDB(t, "order")
	r := validTccBranch(1)
	r.branchID = strings.Repeat("b", 64)
	require.NoError(t, insertTccBranch(db, r), "branch_id must hold what seckill tcc_barrier.branch_id holds (64)")
}

func TestOrderSchema_TccBranchRejects(t *testing.T) {
	db := migratedDB(t, "order")
	cases := []struct {
		name       string
		mutate     func(*tccBranchRow)
		constraint string
	}{
		{"status out of 0~2", func(r *tccBranchRow) { r.status = 3 }, "ck_tcc_branch_status"},
		{"unknown action", func(r *tccBranchRow) { r.action = "undo" }, "ck_tcc_branch_action"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validTccBranch(int64(200 + i))
			tc.mutate(&r)
			requireViolation(t, insertTccBranch(db, r), checkViolation, tc.constraint)
		})
	}
}
