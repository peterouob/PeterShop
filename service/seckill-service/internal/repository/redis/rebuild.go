package redis

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/peterouob/seckill_service/pkg/cache/rediskey"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	"github.com/redis/go-redis/v9"
)

//go:embed lua/rebuild_swap.lua
var swapSrc string

var swapScript = redis.NewScript(swapSrc)

const (
	rebuildBatch     = 1000
	scratchTTL       = 10 * time.Minute
	swapMismatchCode = "REBUILD_TMP_MISMATCH"
)

var ErrRebuildMismatch = errors.New("redis: rebuild scratch set does not match snapshot, nothing swapped")

func (r *StockRepo) Restore(ctx context.Context, snap domain.ActivitySnapshot) error {
	if err := validSnapShot(snap); err != nil {
		return err
	}

	aid := snap.ActivityID
	suffix := fmt.Sprintf(":%016x", rand.Uint64())
	usersTmp := rediskey.UsersRebuild(aid) + suffix
	slotsTmp := rediskey.SlotsRebuild(aid) + suffix

	if len(snap.Users) > 0 || len(snap.Slots) > 0 {
		if _, err := r.redisClient.Pipelined(ctx, func(p redis.Pipeliner) error {
			fillScratch(ctx, p, usersTmp, snap.Users)
			fillScratch(ctx, p, slotsTmp, snap.Slots)
			return nil
		}); err != nil {
			return err
		}
	}

	sealed := "0"
	if snap.Sealed {
		sealed = "1"
	}

	keys := []string{rediskey.Users(aid), usersTmp, rediskey.Slots(aid), slotsTmp, rediskey.Meta(aid)}
	if err := swapScript.Run(ctx, r.redisClient, keys, snap.StartAt.UnixMilli(), snap.EndAt.UnixMilli(),
		len(snap.Users), len(snap.Slots), sealed).Err(); err != nil {
		if strings.Contains(err.Error(), swapMismatchCode) {
			return fmt.Errorf("swap script mismatch: %w, %w", err, ErrRebuildMismatch)
		}
		return err
	}

	return nil
}

func fillScratch[T any](ctx context.Context, p redis.Pipeliner, key string, vals []T) {
	for lo := 0; lo < len(vals); lo += rebuildBatch {
		hi := min(lo+rebuildBatch, len(vals))
		members := make([]any, hi-lo)
		for i, v := range vals[lo:hi] {
			members[i] = v
		}

		p.SAdd(ctx, key, members...)
		if lo == 0 {
			p.PExpire(ctx, key, scratchTTL)
		}
	}
}

func validSnapShot(snapShot domain.ActivitySnapshot) error {
	if snapShot.StartAt.IsZero() || snapShot.EndAt.IsZero() {
		return errors.New("zero start/end time")
	}

	if !snapShot.StartAt.Before(snapShot.EndAt) {
		return errors.New("start time must be earlier than end time")
	}

	for _, s := range snapShot.Slots {
		if s <= 0 {
			return fmt.Errorf("non-positive slot %d", s)
		}
	}

	return nil
}
