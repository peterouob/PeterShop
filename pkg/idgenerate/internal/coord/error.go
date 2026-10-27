package coord

import "errors"

var (
	ErrTTLBeZero      = errors.New("coord: ttl must not be zero")
	ErrGrantLease     = errors.New("coord: grant lease failed")
	ErrLeaseNotFound  = errors.New("coord: lease not found")
	ErrKeyHadOccupied = errors.New("coord: key had occupied")
	ErrLeaseLost      = errors.New("coord: lease lost")
)
