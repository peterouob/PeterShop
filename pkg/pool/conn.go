package pool

import (
	"google.golang.org/grpc"
)

type Conn interface {
	Value() *grpc.ClientConn
}

type conn struct {
	cc   *grpc.ClientConn
	pool *pool
	once bool
}

var _ Conn = (*conn)(nil)

func (c *conn) Value() *grpc.ClientConn {
	return c.cc
}

func (c *conn) reset() error {
	cc := c.cc
	c.cc = nil
	c.once = false
	if cc != nil {
		return cc.Close()
	}
	return nil
}
