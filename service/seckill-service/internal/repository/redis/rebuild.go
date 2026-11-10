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
	swapMismatchCode = "REBUILD_TMP_MISMATCH"
)

var ErrRebuildMismatch = errors.New("redis: rebuild scratch set does not match snapshot, nothing swapped")

func (r *StockRepo) Restore(ctx context.Context, snap domain.ActivitySnapshot) error {
	if err := validSnapShot(snap); err != nil {
		return err
	}

	scratch := fmt.Sprintf("%s:%016x", rediskey.UsersRebuild(snap.ActivityID), rand.Uint64())

	if len(snap.Users) > 0 {
		if _, err := r.redisClient.Pipelined(ctx, func(p redis.Pipeliner) error {
			for lo := 0; lo < len(snap.Users); lo += rebuildBatch {
				hi := min(lo+rebuildBatch, len(snap.Users))
				members := make([]any, hi-lo)
				for i, uid := range snap.Users[lo:hi] {
					members[i] = uid
				}

				p.SAdd(ctx, scratch, members...)
				if lo == 0 {
					p.PExpire(ctx, scratch, 10*time.Minute)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	sealed := "0"
	if snap.Sealed {
		sealed = "1"
	}

	keys := []string{
		rediskey.Users(snap.ActivityID),
		scratch,
		rediskey.Stock(snap.ActivityID),
		rediskey.Meta(snap.ActivityID),
	}

	if err := swapScript.Run(ctx, r.redisClient, keys, snap.Available, snap.StartAt.UnixMilli(),
		snap.EndAt.UnixMilli(), len(snap.Users), sealed).Err(); err != nil {
		if strings.Contains(err.Error(), swapMismatchCode) {
			return fmt.Errorf("swap script mismatch: %w, %w", err, ErrRebuildMismatch)
		}
		return err
	}

	return nil
}

func validSnapShot(snapShot domain.ActivitySnapshot) error {
	if snapShot.Available < 0 {
		return fmt.Errorf("negative available %d", snapShot.Available)
	}

	if snapShot.StartAt.IsZero() || snapShot.EndAt.IsZero() {
		return errors.New("zero start/end time")
	}

	if !snapShot.StartAt.Before(snapShot.EndAt) {
		return errors.New("start time must be earlier than end time")
	}

	return nil
}
