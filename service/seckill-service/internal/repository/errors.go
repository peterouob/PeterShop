package repository

import (
	"errors"
	"fmt"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
)

type deductCode int64

var (
	ErrStockLost   = errors.New("seckill stock key is lost")
	ErrUnknownCode = errors.New("unknown buy.lua result code")
)

const (
	deductSoldOut      deductCode = 0
	deductPass                    = 1
	deductDuplicated              = 2
	deductNotPreheated            = -1
	deductNotOpen                 = -2
	deductSealed                  = -3
	deductStockLost               = -4
)

var deductErrs = map[deductCode]error{
	deductPass:         nil,
	deductNotOpen:      domain.ErrNotOpen,
	deductSealed:       domain.ErrSealed,
	deductNotPreheated: domain.ErrNotPreheated,
	deductSoldOut:      domain.ErrSoldOut,
	deductDuplicated:   domain.ErrAlreadyAdmitted,
	deductStockLost:    ErrStockLost,
}

func (c deductCode) Err() error {
	if err, ok := deductErrs[c]; ok {
		return err
	}
	return fmt.Errorf("%w: code=%d", ErrUnknownCode, c)
}
