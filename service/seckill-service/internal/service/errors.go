package service

import "errors"

var (
	ErrNotStarted    = errors.New("seckill activity has not started")
	ErrSoldOut       = errors.New("product is sold out")
	ErrAlreadyBought = errors.New("user has already bought this product")
)
