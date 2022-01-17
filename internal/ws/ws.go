package ws

import (
	"context"
	"net"

	"github.com/cornelk/hashmap"
	"github.com/go-faster/errors"
	"github.com/phuslu/log"
	"github.com/segmentio/encoding/json"
	"github.com/valyala/fasthttp"
	"github.com/xenking/websocket"

	"github.com/xenking/exchange-emulator/gen/proto/api"
)

type Server struct {
	websocket.Server
	clients *hashmap.HashMap // map[userId] *websocket.Conn
}

func New(ctx context.Context) *Server {
	s := &Server{
		clients: &hashmap.HashMap{},
	}
	s.Server.HandleOpen(s.OpenConn)
	s.Server.HandleClose(s.CloseConn)
	s.Server.HandleData(s.OnData)

	return s
}

func (s *Server) Serve(ln net.Listener) error {
	return fasthttp.Serve(ln, s.Upgrade)
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

func (s *Server) OpenConn(conn *websocket.Conn) {
	log.Info().Uint64("user", conn.ID()).Msg("Open conn")
	conn.SetUserValue("init", false)
}

func (s *Server) CloseConn(conn *websocket.Conn, err error) {
	if err != nil {
		_, _ = conn.Write(NewError(err).Bytes())
	}
	id := conn.UserValue("id")
	s.clients.Del(id)
	log.Info().Str("user", id.(string)).Msg("Close conn")
}

type initConn struct {
	UserID      string `json:"user_id"`
	Initialized bool   `json:"initialized,omitempty"`
}

var (
	ErrAlreadyInit   = errors.New("already initialized")
	ErrInvalidUserID = errors.New("invalid user id")
)

func (s *Server) OnData(conn *websocket.Conn, _ bool, data []byte) {
	isInit := conn.UserValue("init")
	if init, ok := isInit.(bool); init && ok {
		_, _ = conn.Write(NewError(ErrAlreadyInit).Bytes())

		return
	}

	init := &initConn{}
	err := json.Unmarshal(data, init)
	if err != nil {
		_, _ = conn.Write(NewError(err).Bytes())

		return
	}
	if init.UserID == "" {
		_, _ = conn.Write(NewError(ErrInvalidUserID).Bytes())

		return
	}

	conn.SetUserValue("init", true)
	conn.SetUserValue("user", init.UserID)
	s.clients.Set(init.UserID, conn)
	init.Initialized = true

	err = json.NewEncoder(conn).Encode(init)
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
