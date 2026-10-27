package idgenerate

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
)

const epochMs int64 = 1787356800000

type Snowflake struct {
	coord coord.Coordinator
	clock clock
	cur   atomic.Pointer[session]
}

func NewSnowflake(coord coord.Coordinator) *Snowflake {
	return &Snowflake{
		coord: coord,
		clock: newClock(time.Now),
	}
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
