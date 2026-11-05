package domain

import "errors"

var (
	ErrSoldOut       = errors.New("seckill activity is sold out")
	ErrAlreadyBought = errors.New("user has already bought in this activity")
	ErrNotPreheated  = errors.New("seckill activity is not preheated")
	ErrNotOpen       = errors.New("seckill activity is not open")
	ErrSealed        = errors.New("seckill activity is sealed")
)
