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
)

type WithUserID interface {
	GetUserId() string
}

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

func (s *Server) OnUserUpdate(update WithUserID) {
	c, ok := s.clients.Get(update.GetUserId())
	if !ok {
		return
	}
	conn, ok2 := c.(*websocket.Conn)
	if !ok2 {
		return
	}
	err := json.NewEncoder(conn).Encode(update)
	if err != nil {
		_, _ = conn.Write(NewError(err).Bytes())

		return
	}
}

func (s *Server) OnBroadcastUpdate(obj interface{}) {
	for kv := range s.clients.Iter() {
		conn := kv.Value.(*websocket.Conn)

		err := json.NewEncoder(conn).Encode(obj)
		if err != nil {
			_, _ = conn.Write(NewError(err).Bytes())

			return
		}
	}
}

func (s *Server) OpenConn(conn *websocket.Conn) {
	log.Info().Uint64("id", conn.ID()).Msg("Open conn")
	conn.SetUserValue("init", false)
}

func (s *Server) CloseConn(conn *websocket.Conn, err error) {
	if err != nil {
		_, _ = conn.Write(NewError(err).Bytes())
	}
	id := conn.UserValue("id")
	if id == nil {
		return
	}
	s.clients.Del(id)
	log.Info().Uint64("id", conn.ID()).Str("user", id.(string)).Msg("Close conn")
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
	if init, ok := isInit.(bool); ok && init {
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
	log.Info().Uint64("id", conn.ID()).Str("user", init.UserID).Msg("Init conn")

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
