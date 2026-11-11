package postgresql

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pgErr(code, constraint string) error {
	return &pgconn.PgError{Code: code, ConstraintName: constraint}
}

func TestClassifyReserveErr(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want error // nil = no sentinel expected, only "is an error"
	}{
		{"already bought", fmt.Errorf("insert ledger: %w", pgErr("23505", "uk_ledger_activity_user")), domain.ErrAlreadyBought},
		{"slot taken", pgErr("23505", "uk_ledger_active_slot"), domain.ErrSlotTaken},
		{"slot not found", pgErr("23503", "fk_ledger_slot"), domain.ErrSlotNotFound},
		{"slot disabled", pgErr("23514", "ck_ledger_slot_enabled"), domain.ErrSlotDisabled},
		{"order_no collides in outbox", pgErr("23505", "uk_outbox_order_no"), nil},
		{"order_no collides in ledger pk", pgErr("23505", "stock_ledger_pkey"), nil},
		{"other check", pgErr("23514", "ck_activity_window"), nil},
		{"not null", pgErr("23502", ""), nil},
		{"network", errors.New("conn reset"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyReserveErr(tc.in)
			require.Error(t, got)
			if tc.want != nil {
				require.ErrorIs(t, got, tc.want)
				return
			}
			_, isBusiness := errors.AsType[*domain.Error](got)
			assert.False(t, isBusiness, "a system error must never look like a business result")
			assert.ErrorIs(t, got, tc.in, "keep the cause for logs and alerts")
		})
	}
}

func TestClassifyReserveErr_Nil(t *testing.T) {
	assert.NoError(t, classifyReserveErr(nil))
}
