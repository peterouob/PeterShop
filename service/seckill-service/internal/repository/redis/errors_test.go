package redis

import (
	"testing"

	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeductCodeErr(t *testing.T) {
	cases := []struct {
		code int64
		want error
	}{
		{1, nil},
		{0, domain.ErrSoldOut},
		{2, domain.ErrAlreadyAdmitted},
		{-1, domain.ErrNotPreheated},
		{-2, domain.ErrNotOpen},
		{-3, domain.ErrSealed},
		{-4, ErrStockLost},
		{99, ErrUnknownCode},
	}
	for _, tc := range cases {
		got := deductCode(tc.code).Err()
		if tc.want == nil {
			assert.NoError(t, got, "code %d", tc.code)
			continue
		}
		require.ErrorIs(t, got, tc.want, "code %d", tc.code)
	}
}

func TestDeductCodeErr_UnknownKeepsCode(t *testing.T) {
	assert.Contains(t, deductCode(99).Err().Error(), "code=99")
}
