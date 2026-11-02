//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type activityRow struct {
	activityID     int64
	productID      string
	price          int64
	startAt, endAt time.Time
	status         int16
}

func validActivity(id int64) activityRow {
	start := time.Now().Add(time.Hour)
	return activityRow{activityID: id, productID: "p1", price: 100, startAt: start, endAt: start.Add(time.Hour), status: 0}
}

func insertActivity(db *sql.DB, r activityRow) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,now(),now())`,
		r.activityID, r.productID, r.price, r.startAt, r.endAt, r.status)
	return err
}

func seedActivities(t *testing.T, db *sql.DB, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		require.NoError(t, insertActivity(db, validActivity(id)))
	}
}

type stockRow struct {
	activityID                     int64
	total, available, locked, sold int
}

func insertStock(db *sql.DB, r stockRow) error {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO activity_stock (activity_id, total, available, locked, sold) VALUES ($1,$2,$3,$4,$5)`,
		r.activityID, r.total, r.available, r.locked, r.sold)
	return err
}

func TestSeckillSchema_StockRejects(t *testing.T) {
	db := seckillDB(t)
	seedActivities(t, db, 1, 2, 3)
	cases := []struct {
		name       string
		row        stockRow
		constraint string
	}{
		{"negative available but the sum is still equal", stockRow{1, 1, -1, 2, 0}, "ck_stock_non_negative"},
		{"sum not equal to total", stockRow{2, 10, 5, 0, 0}, "ck_stock_i2"},
		{"zero quota activity", stockRow{3, 0, 0, 0, 0}, "ck_stock_bigger_zero"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireViolation(t, insertStock(db, tc.row), checkViolation, tc.constraint)
		})
	}
}

func TestSeckillSchema_StockMoveKeepsI2(t *testing.T) {
	db := seckillDB(t)
	seedActivities(t, db, 1)
	require.NoError(t, insertStock(db, stockRow{1, 10, 10, 0, 0}))

	_, err := db.ExecContext(context.Background(),
		`UPDATE activity_stock SET available = available - 1, locked = locked + 1 WHERE activity_id = $1`, 1)
	require.NoError(t, err)
}

func TestSeckillSchema_StockOneSidedUpdateRejected(t *testing.T) {
	db := seckillDB(t)
	seedActivities(t, db, 1)
	require.NoError(t, insertStock(db, stockRow{1, 10, 10, 0, 0}))

	_, err := db.ExecContext(context.Background(),
		`UPDATE activity_stock SET available = available - 1 WHERE activity_id = $1`, 1)
	requireViolation(t, err, checkViolation, "ck_stock_i2")
}

func TestSeckillSchema_StockOnePerActivity(t *testing.T) {
	db := seckillDB(t)
	seedActivities(t, db, 1, 2)
	require.NoError(t, insertStock(db, stockRow{1, 10, 10, 0, 0}))

	requireViolation(t, insertStock(db, stockRow{1, 5, 5, 0, 0}), uniqueViolation, "activity_stock_pkey")
	require.NoError(t, insertStock(db, stockRow{2, 5, 5, 0, 0}), "next activity for the same product")
}

func TestSeckillSchema_StockRequiresActivity(t *testing.T) {
	db := seckillDB(t)
	requireViolation(t, insertStock(db, stockRow{999, 10, 10, 0, 0}), foreignKeyViolation, "activity_stock_activity_id_fkey")
}

func TestSeckillSchema_ActivityUnique(t *testing.T) {
	db := seckillDB(t)
	seedActivities(t, db, 1)
	requireViolation(t, insertActivity(db, validActivity(1)), uniqueViolation, "seckill_activity_pkey")
}

func TestSeckillSchema_ActivityAcceptsEveryLifecycleStatus(t *testing.T) {
	db := seckillDB(t)
	for status := int16(0); status <= 5; status++ {
		r := validActivity(int64(200 + status))
		r.status = status
		require.NoError(t, insertActivity(db, r), "status=%d", status)
	}
}

func TestSeckillSchema_ActivityRejects(t *testing.T) {
	db := seckillDB(t)
	cases := []struct {
		name       string
		mutate     func(*activityRow)
		constraint string
	}{
		{"price should not be zero", func(r *activityRow) { r.price = 0 }, "ck_activity_price_positive"},
		{"end_at equal to start_at", func(r *activityRow) { r.endAt = r.startAt }, "ck_activity_window"},
		{"status should be in 0~5", func(r *activityRow) { r.status = 6 }, "ck_activity_status"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validActivity(int64(100 + i))
			tc.mutate(&r)
			requireViolation(t, insertActivity(db, r), checkViolation, tc.constraint)
		})
	}
}
