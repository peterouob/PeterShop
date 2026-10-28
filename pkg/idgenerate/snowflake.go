package idgenerate

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/etcd"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	epochMs    int64 = 1787356800000
	maxBackoff       = 5 * time.Second
)

type Snowflake struct {
	coord coord.Coordinator
	clock clock
	cur   atomic.Pointer[session]

	cancel context.CancelFunc
	done   chan struct{}
}

func NewEtcdCoordinator(client *clientv3.Client, prefix string) coord.Coordinator {
	return etcd.New(client, prefix)
}

func New(ctx context.Context, c coord.Coordinator) (*Snowflake, error) {
	snowFlake := &Snowflake{
		coord: c,
		clock: newClock(time.Now),
		done:  make(chan struct{}, 1),
	}

	backoff := 100 * time.Millisecond
	for attempt := 1; ; attempt++ {
		if err := snowFlake.start(ctx); err == nil {
			loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
			snowFlake.cancel = cancel
			go snowFlake.startLoop(loopCtx, tickInterval)

			return snowFlake, nil
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		t := time.NewTimer(backoff/2 + rand.N(backoff/2))
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return nil, fmt.Errorf("snowflake error after %d attempts, err: %w", attempt, ctx.Err())
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

func (sf *Snowflake) Ready() bool {
	cur := sf.cur.Load()
	if cur == nil {
		return false
	}
	if err := cur.admit(sf.clock.mono()); err != nil {
		return false
	}

	return true
}

func (sf *Snowflake) Close(ctx context.Context) error {

	if sf.done != nil {
		sf.cancel()
		select {
		case <-sf.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	cur := sf.cur.Load()
	if cur == nil {
		return nil
	}

	lastMS, ok := cur.close()
	if !ok {
		return nil
	}

	sf.shutdown(ctx, cur, lastMS)
	return nil
}

var (
	ErrNotReady = errors.New("snowflake: the clock time not ready")
)

func (sf *Snowflake) Next() (int64, error) {
	cur := sf.cur.Load()
	if cur == nil {
		return -1, ErrNotReady
	}

	if err := cur.admit(sf.clock.mono()); err != nil {
		return -1, err
	}

	id, err := cur.gen.next(sf.clock.wallMs(), cur.reserve.until())
	if err != nil {
		return -1, err
	}

	return id, nil
}
