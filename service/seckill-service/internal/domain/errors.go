package domain

type Error struct {
	msg string
}

func (e *Error) Error() string { return e.msg }

var (
	ErrSoldOut       = &Error{"seckill activity is sold out"}
	ErrAlreadyBought = &Error{"user has already bought in this activity"}
	ErrNotPreheated  = &Error{"seckill activity is not preheated"}
	ErrNotOpen       = &Error{"seckill activity is not open"}
	ErrSealed        = &Error{"seckill activity is sealed"}
)
