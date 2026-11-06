package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestError_CategoryAndIdentity(t *testing.T) {
	wrapped := fmt.Errorf("seckill buy activity=1: %w", ErrSoldOut)

	got, ok := errors.AsType[*Error](wrapped)
	assert.True(t, ok, "a wrapped business error is still found by AsType")
	assert.Same(t, ErrSoldOut, got)
	assert.ErrorIs(t, wrapped, ErrSoldOut)
	assert.NotErrorIs(t, wrapped, ErrAlreadyAdmitted, "outcomes are distinct even though they share a type")

	_, ok = errors.AsType[*Error](errors.New("conn reset"))
	assert.False(t, ok, "system failures are not business errors")
}
