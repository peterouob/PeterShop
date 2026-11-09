package postgresql

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
)

const NoDataFound = "P0002"

var (
	ErrUnexpectedRows   = errors.New("unexpected affected rows")
	ErrDuplicateOrderNo = errors.New("duplicate order_no")
)

var constraintErrs = map[string]error{
	"uk_ledger_activity_user": domain.ErrAlreadyBought,
	"ck_stock_non_negative":   domain.ErrSoldOut,
}

func classifyReserveErr(err error) error {
	if err == nil {
		return nil
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		if biz, ok := constraintErrs[pgErr.ConstraintName]; ok {
			return biz
		}
	}
	return fmt.Errorf("reserve: %w", err)
}
