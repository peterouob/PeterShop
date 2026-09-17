package pool

import (
	"context"
	"errors"
	"fmt"

	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/peterouob/seckill_service/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

type Pool interface {
	Get() (Conn, error)
	Put(Conn) error
	Close() error
	Status() string
}

type pool struct {
	index     atomic.Uint32
	curr      atomic.Int32
	ref       atomic.Int32
	opt       Option
	conns     []*conn
	addr      string
	closed    atomic.Int32
	checkTime time.Duration
	cancel    context.CancelFunc
	sync.RWMutex
}

var _ Pool = (*pool)(nil)

func New(addr string, opt Option) Pool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	p := &pool{
		opt:       opt,
		conns:     make([]*conn, opt.MaxActive),
		addr:      addr,
		checkTime: time.Minute,
		cancel:    cancel,
	}
	p.curr.Store(opt.MaxIdle)

	for i := range opt.MaxIdle {
		c, err := opt.Dial(addr)
		if err != nil {
			panic(fmt.Sprintf("error in dial %s , %v", addr, err.Error()))
		}
		p.conns[i] = p.wrapConn(c, false)
	}

	logger.Logf("new pool success %v\n", p.Status())
	p.checkHealthy(ctx)
	return p
}

func (p *pool) Get() (Conn, error) {
	cur := p.curr.Load()
	if cur == 0 {
		return nil, errors.New("pool closed")
	}
	p.incRef()
	nextRef := p.ref.Load()

	if nextRef <= cur*p.opt.MaxConcurrentStreams {
		next := p.index.Add(1) % uint32(cur)
		return p.conns[next], nil
	}

	if cur == p.opt.MaxActive {
		if p.opt.Reuse {
			next := p.index.Add(1) % uint32(cur)
			return p.conns[next], nil
		}
		c, err := p.opt.Dial(p.addr)
		if err != nil {
			p.decRef()
			return nil, err
		}
		return p.wrapConn(c, true), nil
	}

	p.Lock()
	defer p.Unlock()
	cur = p.curr.Load()
	if cur < p.opt.MaxActive && nextRef > cur*p.opt.MaxConcurrentStreams {
		inc := cur
		if cur+inc > p.opt.MaxActive {
			inc = p.opt.MaxActive - cur
		}

		var err error
		var i int
		for i = range make([]struct{}, inc) {
			c, er := p.opt.Dial(p.addr)
			if er != nil {
				err = er
				break
			}
			p.reset(cur + int32(i))
			p.conns[cur+int32(i)] = p.wrapConn(c, false)
		}

		cur += int32(i)
		log.Printf("grow pool: %d ---> %d, increment: %d, maxActive: %d\n",
			p.curr.Load(), cur, inc, p.opt.MaxActive)
		p.curr.Store(cur)
		if err != nil {
			return nil, err
		}
	}
	next := p.index.Add(1) % uint32(cur)
	return p.conns[next], nil
}

func (p *pool) Put(c Conn) error {
	if c == nil {
		return nil
	}

	defer p.decRef()

	if cc, ok := c.(*conn); ok && cc.once {
		return cc.cc.Close()
	}
	return nil
}

func (p *pool) Close() error {
	p.cancel()
	p.index.Store(0)
	p.curr.Store(0)
	p.ref.Store(0)
	p.closed.Store(1)
	p.delete(0)
	log.Printf("pool closed")
	return nil
}

func (p *pool) Status() string {
	return fmt.Sprintf("address:%s, index:%d, current:%d, ref:%d. option:%v",
		p.addr, p.index.Load(), p.curr.Load(), p.ref.Load(), p.opt)
}

func (p *pool) incRef() {
	p.ref.Add(1)
	if p.ref.Load() == math.MaxInt32 {
		panic("ref overflow")
	}
}

func (p *pool) decRef() {
	newRef := p.ref.Add(-1)
	if atomic.LoadInt32(&newRef) < 0 && p.closed.Load() == 0 {
		panic("ref overflow to negative")
	}

	if atomic.LoadInt32(&newRef) == 0 && p.curr.Load() > p.opt.MaxIdle {
		if p.TryLock() {
			defer p.Unlock()
			log.Printf("shrink pool: %d ---> %d, decrement: %d, maxActive: %d\n",
				p.curr.Load(), p.opt.MaxIdle, p.curr.Load()-p.opt.MaxIdle, p.opt.MaxActive)

			p.delete(p.opt.MaxIdle)
			p.curr.Store(p.opt.MaxIdle)
		}
	}
}

func (p *pool) reset(idx int32) {
	conn := p.conns[idx]
	if conn == nil {
		return
	}
	err := conn.reset()
	if err != nil {
		log.Printf("reset pool conn err:%v\n", err)
	}
	p.conns[idx] = nil
}

func (p *pool) delete(begin int32) {
	for i := begin; i < p.opt.MaxActive; i++ {
		p.reset(i)
	}
}

func (p *pool) wrapConn(cc *grpc.ClientConn, once bool) *conn {
	return &conn{cc: cc, pool: p, once: once}
}

func (p *pool) checkHealthy(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(p.checkTime)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.reConnect()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (p *pool) reConnect() {
	active := p.curr.Load()
	for i := range active {
		c := p.conns[i]
		if c == nil || c.Value() == nil {
			continue
		}

		state := c.Value().GetState()
		if state == connectivity.Shutdown || state == connectivity.TransientFailure {
			log.Printf("reconnect pool conn[%d] is shutdown", i)
			newCC, err := p.opt.Dial(p.addr)
			if err != nil {
				log.Printf("reconnect pool conn[%d] error:%v", i, err)
				continue
			}

			p.Lock()
			oldCC := p.conns[i]
			p.conns[i] = p.wrapConn(newCC, false)
			p.Unlock()

			if oldCC != nil {
				_ = p.Put(oldCC)
			}
		}
	}
}
