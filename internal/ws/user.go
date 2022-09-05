package ws

import (
	"github.com/goccy/go-json"
	"github.com/xenking/websocket"
)

type UserConn struct {
	conn  *websocket.Conn
	enc   *json.Encoder
	close chan struct{}

	ID string
}

func (c *UserConn) SendJSON(data interface{}) error {
	if c == nil {
		return nil
	}

	err := c.enc.EncodeWithOption(data,
		json.DisableHTMLEscape(),
		json.DisableNormalizeUTF8(),
		json.UnorderedMap(),
	)
	if err != nil {
		_, _ = c.conn.Write(NewError(err).Bytes())
	}

	return err
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

	defer close(c.close)

	return c.conn.Close()
}

func (c *UserConn) CloseHandler() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.close
}
