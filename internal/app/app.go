package app

import (
	"context"
	"time"

	"github.com/cornelk/hashmap"
	"github.com/go-faster/errors"
	"github.com/phuslu/log"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/internal/exchange"
	"github.com/xenking/exchange-emulator/internal/parser"
	"github.com/xenking/exchange-emulator/internal/ws"
	"github.com/xenking/exchange-emulator/pkg/logger"
)

type App struct {
	parser   *parser.Parser
	config   *config.Config
	orders   <-chan *ws.UserConn
	prices   <-chan *ws.UserConn
	shutdown chan shutdownHandler
	clients  *hashmap.Map[string, *exchange.Client]
}

func New(orders, prices <-chan *ws.UserConn, cfg *config.Config) (*App, error) {
	p, err := parser.New(cfg.Parser)

	app := &App{
		parser:   p,
		config:   cfg,
		orders:   orders,
		prices:   prices,
		shutdown: make(chan shutdownHandler, 128),
		clients:  hashmap.New[string, *exchange.Client](),
	}

	return app, err
}

func (a *App) Start(ctx context.Context) {
	go a.startShutdownHandler(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case conn := <-a.orders:
			client, err := a.GetOrCreateClient(ctx, conn.ID)
			if err != nil {
				conn.SendError(err)
				conn.Close()
				continue
			}

			client.SetOrdersConnection(conn)
		case conn := <-a.prices:
			client, err := a.GetOrCreateClient(ctx, conn.ID)
			if err != nil {
				conn.SendError(err)
				conn.Close()
				continue
			}

			client.SetPricesConnection(conn)
		}
	}
}

func (a *App) GetClient(userID string) (*Client, error) {
	c, ok := a.clients.Get(userID)
	if !ok {
		return nil, errors.New("client not found")
	}
	return &Client{Client: c}, nil
}

func (a *App) GetOrCreateClient(ctx context.Context, userID string) (*Client, error) {
	client, ok := a.clients.Get(userID)
	if !ok {

		listener := a.parser.NewListener()
		client = exchange.New(ctx, a.config, listener, logger.NewUser(userID))

		log.Debug().Str("user", userID).Msg("new exchange client")
		a.clients.Set(userID, client)
		a.shutdown <- shutdownHandler{
			shutdown: client.Shutdown(),
			clientID: userID,
		}
	}

	return &Client{Client: client}, nil
}

type shutdownHandler struct {
	shutdown <-chan struct{}
	clientID string
}

func (a *App) startShutdownHandler(ctx context.Context) {
	var clients []shutdownHandler

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case shutdown := <-a.shutdown:
			clients = append(clients, shutdown)
		case <-ticker.C:
			for i := len(clients) - 1; i >= 0; i-- {
				select {
				case <-clients[i].shutdown:
					a.clients.Del(clients[i].clientID)
					log.Debug().Str("user", clients[i].clientID).Msg("client shutdown")
					clients = append(clients[:i], clients[i+1:]...)
				default:
				}
			}
		}
	}
}
