package rediskey

import "fmt"

const (
	OrderExpiryQueue = "seckill:delay:order-payment-timeout"
	OutboxGroup      = "relay"
)

func Meta(aid int64) string      { return fmt.Sprintf("seckill:{%d}:meta", aid) }
func Stock(aid int64) string     { return fmt.Sprintf("seckill:{%d}:stock", aid) }
func Users(aid int64) string     { return fmt.Sprintf("seckill:{%d}:users", aid) }
func Outbox(aid int64) string    { return fmt.Sprintf("seckill:{%d}:outbox", aid) }
func Released(aid int64) string  { return fmt.Sprintf("seckill:{%d}:released", aid) }
func Limit(userID string) string { return fmt.Sprintf("seckill:limit:%s", userID) }
