//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func seedSlots(t *testing.T, db *sql.DB, activityID int64, n int) {
	t.Helper()
	seedActivities(t, db, activityID)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO activity_slot (activity_id, slot_no) SELECT $1, g FROM generate_series(1, $2::int) g`, activityID, n)
	require.NoError(t, err)
}

func disableSlot(db execer, activityID int64, slotNo int32) error {
	_, err := db.ExecContext(context.Background(),
		`UPDATE activity_slot SET disabled_at = now() WHERE activity_id = $1 AND slot_no = $2`, activityID, slotNo)
	return err
}

func setLedger(db execer, orderNo int64, status int16, slotNo int32) error {
	_, err := db.ExecContext(context.Background(),
		`UPDATE stock_ledger SET status = $2, slot_no = $3, updated_at = now() WHERE order_no = $1`, orderNo, status, slotNo)
	return err
}

func onSlot(orderNo int64, slotNo int32, status int16) ledgerRow {
	r := ledgerFor(orderNo, status)
	r.slotNo = slotNo
	return r
}

func mustInsert(t *testing.T, db execer, r ledgerRow) {
	t.Helper()
	_, err := insertLedgerRow(db, r, "")
	require.NoError(t, err)
}

func beginBlocked(t *testing.T, db *sql.DB, op func(*sql.Tx) error) (*sql.Tx, <-chan error) {
	t.Helper()
	ctx := context.Background()
	tx := beginTx(t, db)
	var pid int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))

	done := make(chan error, 1)
	go func() { done <- op(tx) }()

	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRowContext(ctx,
			`SELECT wait_event_type = 'Lock' FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "op never blocked on a lock")
	return tx, done
}

func beginTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func TestSlot_OneActiveRowPerSlot(t *testing.T) {
	for _, occupant := range []int16{ledgerLocked, ledgerSold} {
		t.Run(statusName(occupant), func(t *testing.T) {
			db := migratedDB(t, "seckill")
			seedSlots(t, db, 1, 1)
			mustInsert(t, db, onSlot(1, 1, occupant))

			_, err := insertLedgerRow(db, onSlot(2, 1, ledgerLocked), "")
			requireViolation(t, err, uniqueViolation, "uk_ledger_active_slot")
		})
	}
}

func TestSlot_MissingSlot(t *testing.T) {
	for _, status := range []int16{ledgerLocked, ledgerSold, ledgerReleased} {
		t.Run(statusName(status), func(t *testing.T) {
			db := migratedDB(t, "seckill")
			seedSlots(t, db, 1, 1)
			_, err := insertLedgerRow(db, onSlot(1, 99, status), "")
			requireViolation(t, err, foreignKeyViolation, "fk_ledger_slot")
		})
	}
}

func TestSlot_ReleasedSlotIsReusable(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedSlots(t, db, 1, 1)
	mustInsert(t, db, onSlot(1, 1, ledgerLocked))
	require.NoError(t, setLedger(db, 1, ledgerReleased, 1))

	mustInsert(t, db, onSlot(2, 1, ledgerLocked))
}

func TestSlot_ReviveOntoTakenSlot(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedSlots(t, db, 1, 1)
	mustInsert(t, db, onSlot(1, 1, ledgerReleased))
	mustInsert(t, db, onSlot(2, 1, ledgerLocked))

	requireViolation(t, setLedger(db, 1, ledgerLocked, 1), uniqueViolation, "uk_ledger_active_slot")
}

func TestSlot_ConcurrentReserveSameSlot(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedSlots(t, db, 1, 1)
	first := beginTx(t, db)
	mustInsert(t, first, onSlot(1, 1, ledgerLocked))

	_, second := beginBlocked(t, db, func(tx *sql.Tx) error {
		_, err := insertLedgerRow(tx, onSlot(2, 1, ledgerLocked), "")
		return err
	})
	require.NoError(t, first.Commit())

	requireViolation(t, <-second, uniqueViolation, "uk_ledger_active_slot")
}

func TestSlot_DeleteReferencedSlotRestricted(t *testing.T) {
	for _, status := range []int16{ledgerLocked, ledgerReleased} {
		t.Run(statusName(status), func(t *testing.T) {
			db := migratedDB(t, "seckill")
			seedSlots(t, db, 1, 1)
			mustInsert(t, db, onSlot(1, 1, status))

			_, err := db.ExecContext(context.Background(), `DELETE FROM activity_slot WHERE activity_id = 1 AND slot_no = 1`)
			requireViolation(t, err, foreignKeyViolation, "fk_ledger_slot")
		})
	}
}

func TestSlot_DisabledSlotRejectsNewHolder(t *testing.T) {
	cases := []struct {
		name string
		do   func(t *testing.T, db *sql.DB) error
	}{
		{"insert", func(t *testing.T, db *sql.DB) error {
			_, err := insertLedgerRow(db, onSlot(2, 1, ledgerLocked), "")
			return err
		}},
		{"revive released row", func(t *testing.T, db *sql.DB) error {
			mustInsert(t, db, onSlot(2, 2, ledgerReleased))
			return setLedger(db, 2, ledgerLocked, 1)
		}},
		{"move locked row", func(t *testing.T, db *sql.DB) error {
			mustInsert(t, db, onSlot(2, 2, ledgerLocked))
			return setLedger(db, 2, ledgerLocked, 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := migratedDB(t, "seckill")
			seedSlots(t, db, 1, 2)
			require.NoError(t, disableSlot(db, 1, 1))

			requireViolation(t, tc.do(t, db), checkViolation, "ck_ledger_slot_enabled")
		})
	}
}

func TestSlot_HolderCanSettleAfterDisable(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedSlots(t, db, 1, 1)
	mustInsert(t, db, onSlot(1, 1, ledgerLocked))
	require.NoError(t, disableSlot(db, 1, 1))

	require.NoError(t, setLedger(db, 1, ledgerSold, 1))
}

func TestSlot_DisableCommittedBeforeReserve(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedSlots(t, db, 1, 1)
	disabler := beginTx(t, db)
	require.NoError(t, disableSlot(disabler, 1, 1))

	_, reserve := beginBlocked(t, db, func(tx *sql.Tx) error {
		_, err := insertLedgerRow(tx, onSlot(1, 1, ledgerLocked), "")
		return err
	})
	require.NoError(t, disabler.Commit())

	requireViolation(t, <-reserve, checkViolation, "ck_ledger_slot_enabled")
}

func TestSlot_DisableWaitsForReserve(t *testing.T) {
	db := migratedDB(t, "seckill")
	seedSlots(t, db, 1, 1)
	reserve := beginTx(t, db)
	mustInsert(t, reserve, onSlot(1, 1, ledgerLocked))

	disabler, disable := beginBlocked(t, db, func(tx *sql.Tx) error { return disableSlot(tx, 1, 1) })
	require.NoError(t, reserve.Commit())

	require.NoError(t, <-disable)
	require.NoError(t, disabler.Commit())
}

func statusName(s int16) string {
	return map[int16]string{ledgerLocked: "locked", ledgerSold: "sold", ledgerReleased: "released"}[s]
}
