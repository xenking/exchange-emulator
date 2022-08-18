package ws

import (
	"github.com/segmentio/encoding/json"
	"github.com/xenking/websocket"
)

type UserConn struct {
	conn  *websocket.Conn
	close chan struct{}

	ID string
}

func (c *UserConn) Send(data interface{}) error {
	err := json.NewEncoder(c.conn).Encode(data)
	if err != nil {
		_, _ = c.conn.Write(NewError(err).Bytes())

	}

	return err
}

func (c *UserConn) SendError(err error) {
	_, _ = c.conn.Write(NewError(err).Bytes())
}

func (c *UserConn) Close() error {
	defer close(c.close)
	return c.conn.Close()
}

func (c *UserConn) CloseHandler() <-chan struct{} {
	return c.close
}
