package application

import (
	"net"

	"github.com/cornelk/hashmap"
	"github.com/pkg/errors"
	"github.com/segmentio/encoding/json"
	"github.com/valyala/fasthttp"
	"github.com/xenking/websocket"
)

type Server struct {
	websocket.Server
	*Core
	clients *hashmap.HashMap
}

func NewServer(core *Core) *Server {
	s := &Server{
		Core:    core,
		clients: &hashmap.HashMap{},
	}
	s.HandleOpen(s.OpenConn)
	s.HandleClose(s.CloseConn)
	s.HandleData(s.OnData)
	s.CompleteHandler = s.OnOrderUpdate

	return s
}

func (s *Server) Serve(ln net.Listener) error {
	return fasthttp.Serve(ln, s.Upgrade)
}

func (s *Server) OpenConn(conn *websocket.Conn) {
	s.clients.Set(conn.ID(), conn)
}

func (s *Server) CloseConn(conn *websocket.Conn, err error) {
	_, _ = conn.Write(NewError(err).Bytes())
	s.clients.Del(conn.ID())
}

type Operation uint8

const (
	OpExchangeStart Operation = iota + 1
	OpExchangeStop
	OpExchangeOffset

	OpExchangeInfo
	OpPrice

	OpBalanceGet
	OpBalanceSet

	OpOrderCreate
	OpOrderGet
	OpOrderCancel

	OpOrderUpdate
)

type op struct {
	Operation Operation `json:"operation"`
}

var ErrInvalidOperation = errors.New("invalid operation")

func (s *Server) OnData(c *websocket.Conn, _ bool, d []byte) {
	o := &op{}
	err := json.Unmarshal(d, o)
	if err != nil {
		_, _ = c.Write(NewError(err).Bytes())

		return
	}
	switch o.Operation {
	case OpPrice:
		s.Exchange.Stop()
		err = s.GetPrice(c, d)
	case OpOrderCreate:
		s.Exchange.Stop()
		err = s.CreateOrder(c, c.ID(), d)
	case OpOrderCancel:
		s.Exchange.Stop()
		err = s.CancelOrder(c, d)
	case OpOrderGet:
		err = s.GetOrder(c, d)
	case OpExchangeInfo:
		s.ExchangeInfo(c)
	case OpBalanceGet:
		err = s.GetBalance(c, c.ID())
	case OpBalanceSet:
		err = s.SetBalance(c, c.ID(), d)
	case OpExchangeStart:
		s.Exchange.Start()
	case OpExchangeStop:
		s.Exchange.Stop()
	case OpExchangeOffset:
		err = s.Exchange.Offset(d)
	case OpOrderUpdate:
		err = ErrInvalidOperation
	}
	if err != nil {
		_, _ = c.Write(NewError(err).Bytes())

		return
	}
}

func (s *Server) OnOrderUpdate(order *Order) {
	c, ok := s.clients.Get(order.UserID)
	if !ok {
		return
	}
	conn, ok2 := c.(*websocket.Conn)
	if !ok2 {
		return
	}
	order.Op = OpOrderUpdate
	err := json.NewEncoder(conn).Encode(order)
	if err != nil {
		_, _ = conn.Write(NewError(err).Bytes())

		return
	}
}
