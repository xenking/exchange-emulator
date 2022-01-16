package ws

import (
	"context"
	"net"

	"github.com/cornelk/hashmap"
	"github.com/phuslu/log"
	"github.com/segmentio/encoding/json"
	"github.com/valyala/fasthttp"
	"github.com/xenking/websocket"

	"github.com/xenking/exchange-emulator/gen/proto/api"
)

type Server struct {
	websocket.Server
	clients *hashmap.HashMap
}

func New(ctx context.Context) *Server {
	s := &Server{
		clients: &hashmap.HashMap{},
	}
	s.HandleOpen(s.OpenConn)
	s.HandleClose(s.CloseConn)

	return s
}

func (s *Server) Serve(ln net.Listener) error {
	return fasthttp.Serve(ln, s.Upgrade)
}

func (s *Server) OpenConn(conn *websocket.Conn) {
	s.clients.Set(conn.ID(), conn)
	log.Info().Uint64("user", conn.ID()).Msg("Open conn")
}

func (s *Server) CloseConn(conn *websocket.Conn, err error) {
	if err != nil {
		_, _ = conn.Write(NewError(err).Bytes())
	}

	s.clients.Del(conn.ID())
	log.Info().Uint64("user", conn.ID()).Msg("Close conn")
}

func (s *Server) OnOrderUpdate(order *api.Order) {
	c, ok := s.clients.Get(order.UserId)
	if !ok {
		return
	}
	conn, ok2 := c.(*websocket.Conn)
	if !ok2 {
		return
	}
	err := json.NewEncoder(conn).Encode(order)
	if err != nil {
		_, _ = conn.Write(NewError(err).Bytes())

		return
	}
}

type Error struct {
	Err error `json:"error"`
}

func NewError(err error) Error {
	return Error{Err: err}
}

func (e Error) Error() string {
	return string(e.Bytes())
}

func (e Error) Bytes() []byte {
	b := append([]byte{}, `{"error":"`...)
	b = append(b, e.Err.Error()...)
	b = append(b, `"}`...)

	return b
}
