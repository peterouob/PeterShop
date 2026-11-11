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

func TestSeckillSchema_ActivityUnique(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedActivities(t, db, 1)
	requireViolation(t, insertActivity(db, validActivity(1)), uniqueViolation, "seckill_activity_pkey")
}

func TestSeckillSchema_ActivityAcceptsEveryLifecycleStatus(t *testing.T) {
	db := migratedDB(t, "seckill")
	for status := int16(0); status <= 5; status++ {
		r := validActivity(int64(200 + status))
		r.status = status
		require.NoError(t, insertActivity(db, r), "status=%d", status)
	}
}

func TestSeckillSchema_ActivityRejects(t *testing.T) {
	db := migratedDB(t, "seckill")
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
