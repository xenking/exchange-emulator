package application

import (
	"context"
	"net"

	"github.com/cornelk/hashmap"
	"github.com/phuslu/log"
	"github.com/pkg/errors"
	"github.com/segmentio/encoding/json"
	"github.com/valyala/fasthttp"
	"github.com/xenking/websocket"

	"github.com/xenking/exchange-emulator/models"
)

type Server struct {
	websocket.Server
	*Core
	clients *hashmap.HashMap

	gctx     context.Context
	dataFile string
}

func NewServer(ctx context.Context, core *Core, dataFile string) *Server {
	s := &Server{
		Core:     core,
		clients:  &hashmap.HashMap{},
		gctx:     ctx,
		dataFile: dataFile,
	}
	s.HandleOpen(s.OpenConn)
	s.HandleClose(s.CloseConn)
	s.HandleData(s.OnData)
	s.Core.SetCallback(s.OnOrderUpdate)

	return s
}

func (s *Server) Serve(ln net.Listener) error {
	return fasthttp.Serve(ln, s.Upgrade)
}

func (s *Server) OpenConn(conn *websocket.Conn) {
	ctx, cancel := context.WithCancel(s.gctx)
	conn.SetUserValue("cancel", cancel)
	if err := s.Core.AddExchange(ctx, conn.ID(), s.dataFile); err != nil {
		return
	}
	s.clients.Set(conn.ID(), conn)
	log.Info().Uint64("user", conn.ID()).Msg("Open conn")
}

func (s *Server) CloseConn(conn *websocket.Conn, err error) {
	_, _ = conn.Write(models.NewError(err).Bytes())
	c := conn.UserValue("cancel")
	if cancel, ok := c.(context.CancelFunc); ok {
		cancel()
	}
	s.clients.Del(conn.ID())
	s.Core.DeleteExchange(conn.ID())
	log.Info().Uint64("user", conn.ID()).Msg("Close conn")
}

var (
	ErrInvalidOperation = errors.New("invalid operation")
	ErrInvalidExchange  = errors.New("invalid exchange")
)

func (s *Server) OnData(c *websocket.Conn, _ bool, d []byte) {
	op := &models.Operation{}
	err := json.Unmarshal(d, op)
	if err != nil {
		_, _ = c.Write(models.NewError(err).Bytes())

		return
	}
	user := c.ID()
	log.Info().Uint64("user", user).Uint8("operation", uint8(op.Op)).Msg("Request")
	//nolint:exhaustive
	switch op.Op {
	case models.OpBalanceGet:
		err = s.GetBalance(c, user)
	case models.OpBalanceSet:
		err = s.SetBalance(c, user, d)
	case models.OpExchangeInfo:
		err = s.ExchangeInfo(c)
	case models.OpOrderGet:
		err = s.GetOrder(c, d)
	case models.OpOrderUpdate:
		err = ErrInvalidOperation
	}
	if err != nil {
		_, _ = c.Write(models.NewError(err).Bytes())

		return
	}

	exchange := s.UserExchange(user)
	if exchange == nil {
		_, _ = c.Write(models.NewError(ErrInvalidExchange).Bytes())

		return
	}
	//nolint:exhaustive
	switch op.Op {
	case models.OpExchangeStart:
		exchange.Start()
	case models.OpExchangeStop:
		exchange.Stop()
	case models.OpExchangeOffset:
		err = exchange.Offset(d)
	case models.OpPrice:
		exchange.Stop()
		err = s.GetPrice(c, user, d)
	case models.OpOrderCreate:
		exchange.Stop()
		err = s.CreateOrder(c, user, d)
	case models.OpOrderCancel:
		exchange.Stop()
		err = s.CancelOrder(c, d)
	}
	if err != nil {
		_, _ = c.Write(models.NewError(err).Bytes())

		return
	}
}

func (s *Server) OnOrderUpdate(order *models.Order) {
	c, ok := s.clients.Get(order.UserID)
	if !ok {
		return
	}
	conn, ok2 := c.(*websocket.Conn)
	if !ok2 {
		return
	}
	order.Op = models.OpOrderUpdate
	err := json.NewEncoder(conn).Encode(order)
	if err != nil {
		_, _ = conn.Write(models.NewError(err).Bytes())

		return
	}
}
