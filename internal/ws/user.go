package ws

import (
	"sync/atomic"

	"github.com/xenking/websocket"
)

type UserConn struct {
	conn   *websocket.Conn
	close  chan struct{}
	ID     string
	closed int32
}

func (c *UserConn) Send(data []byte) error {
	if c == nil {
		return nil
	}
	_, err := c.conn.Write(data)
	return err
}

func (c *UserConn) SendError(err error) {
	_, _ = c.conn.Write(NewError(err).Bytes())
}

func (c *UserConn) Close() error {
	if c == nil {
		return nil
	}

	if atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		close(c.close)
		return c.conn.Close()
	}

	return nil
}

func (c *UserConn) Shutdown() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.close
}
