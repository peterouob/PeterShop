package rediskey

import "fmt"

const (
	DelayOrders = "seckill:delay:order-payment-timeout"
	OutboxGroup = "relay"
)

func Meta(pid string) string     { return fmt.Sprintf("seckill:{%s}:meta", pid) }
func Stock(pid string) string    { return fmt.Sprintf("seckill:{%s}:stock", pid) }
func Users(pid string) string    { return fmt.Sprintf("seckill:{%s}:users", pid) }
func Outbox(pid string) string   { return fmt.Sprintf("seckill:{%s}:outbox", pid) }
func Released(pid string) string { return fmt.Sprintf("seckill:{%s}:released", pid) }
func Limit(userID string) string { return fmt.Sprintf("seckill:limit:%s", userID) }
