package rediskey

import "fmt"

func Meta(aid int64) string  { return fmt.Sprintf("seckill:{%d}:meta", aid) }
func Users(aid int64) string { return fmt.Sprintf("seckill:{%d}:users", aid) }

// UsersRebuild is the scratch set a rebuild fills before swapping it in; same hash tag as Users so RENAME works in a cluster.
func UsersRebuild(aid int64) string { return fmt.Sprintf("seckill:{%d}:users:rebuild", aid) }
func Outbox(aid int64) string       { return fmt.Sprintf("seckill:{%d}:outbox", aid) }
func Released(aid int64) string     { return fmt.Sprintf("seckill:{%d}:released", aid) }
func Limit(userID string) string    { return fmt.Sprintf("seckill:limit:%s", userID) }
func Slots(aid int64) string        { return fmt.Sprintf("seckill:{%d}:slots", aid) }

// SlotsRebuild is the scratch counterpart of Slots, sharing its hash tag.
func SlotsRebuild(aid int64) string { return fmt.Sprintf("seckill:{%d}:slots:rebuild", aid) }
