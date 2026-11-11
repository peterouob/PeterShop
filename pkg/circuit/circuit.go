package circuit

import (
	"sync/atomic"

	"github.com/peterouob/seckill_service/pkg/logger"
	"github.com/sony/gobreaker/v2"
)

const (
	probeBits = 16
	probeMask = 1<<probeBits - 1
)

func pack(epoch, probes uint64) uint64 { return epoch<<probeBits | probes }

func unpack(v uint64) (epoch, probes uint64) {
	return v >> probeBits, v & probeMask
}

type Gate[T any] struct {
	cb        *gobreaker.CircuitBreaker[T]
	word      atomic.Uint64
	maxProbes uint64
}

func NewGate[T any](st gobreaker.Settings) (*Gate[T], error) {
	if st.MaxRequests == 0 || uint64(st.MaxRequests) > probeMask {
		logger.Warnf("invalid the max request auto adjustment %d", st.MaxRequests)
		st.MaxRequests = 1
	}

	g := &Gate[T]{maxProbes: uint64(st.MaxRequests)}

	userHook := st.OnStateChange
	st.OnStateChange = func(name string, from, to gobreaker.State) {
		g.onStateChange()
		if userHook != nil {
			userHook(name, from, to)
		}
	}
	g.cb = gobreaker.NewCircuitBreaker[T](st)
	return g, nil
}

func (g *Gate[T]) Admit() (p Permit[T], ok bool) {
	switch g.cb.State() {
	case gobreaker.StateClosed:
		return Permit[T]{}, true
	case gobreaker.StateOpen:
		return Permit[T]{}, false
	default:
	}

	for {
		old := g.word.Load()
		epoch, probes := unpack(old)
		if probes >= g.maxProbes {
			return Permit[T]{}, false
		}
		if g.word.CompareAndSwap(old, pack(epoch, probes+1)) {
			return Permit[T]{g: g, epoch: epoch}, true
		}
	}
}

func (g *Gate[T]) onStateChange() {
	for {
		old := g.word.Load()
		epoch, _ := unpack(old)
		if g.word.CompareAndSwap(old, pack(epoch+1, 0)) {
			return
		}
	}
}

func (g *Gate[T]) release(want uint64) {
	for {
		old := g.word.Load()
		epoch, probes := unpack(old)
		if epoch != want || probes == 0 {
			return
		}

		if g.word.CompareAndSwap(old, pack(epoch, probes-1)) {
			return
		}
	}
}
