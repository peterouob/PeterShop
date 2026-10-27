package idgenerate

import (
	"errors"
	"sync/atomic"
)

const (
	workerIDBits   = 10
	sequenceBits   = 12
	maxWorkerID    = 1<<workerIDBits - 1
	sequenceMask   = 1<<sequenceBits - 1
	workerIDShift  = sequenceBits
	timestampShift = sequenceBits + workerIDBits
)

const (
	maxAhead     int64 = 1 << 16 // 1.09 min and i think the active will continued 1 min
	maxTimestamp int64 = (1 << 41) - 1
)

var (
	ErrClockAhead           = errors.New("idgenerate: sequence exhausted beyond allowed clock lead")
	ErrReservationExhausted = errors.New("idgenerate: reservation exhausted")
)

type generator struct {
	workerID uint64
	state    atomic.Uint64
}

func (s *generator) next(ts, limit int64) (int64, error) {
	for {
		old := s.state.Load()

		if old == sealedState {
			return 0, ErrClosed
		}

		next := old + 1

		if ts > int64(old)>>sequenceBits {
			next = uint64(ts) << sequenceBits
		}

		nextTS := int64(next) >> sequenceBits

		if nextTS-ts > maxAhead {
			return 0, ErrClockAhead
		}

		if nextTS > limit {
			return 0, ErrReservationExhausted
		}

		if s.state.CompareAndSwap(old, next) {
			return compose(nextTS, s.workerID, next&sequenceMask), nil
		}
	}
}

func compose(timeDiff int64, workerID, sequenceID uint64) int64 {
	return (timeDiff << timestampShift) | (int64(workerID) << workerIDShift) | int64(sequenceID)
}

func decompose(id int64) (timeDiff int64, workerID, sequenceID uint64) {
	timeDiff = id >> timestampShift
	workerID = (uint64(id) >> workerIDShift) & maxWorkerID
	sequenceID = uint64(id) & sequenceMask
	return
}
