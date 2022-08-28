package app

import (
	"context"
	"github.com/cornelk/hashmap"
	"github.com/phuslu/log"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/internal/exchange"
	"github.com/xenking/exchange-emulator/internal/ws"
)

type App struct {
	config  *config.Config
	orders  <-chan *ws.UserConn
	prices  <-chan *ws.UserConn
	close   chan chan (<-chan struct{})
	clients *hashmap.HashMap // map[string]*exchange.Client
}

func New(orders, prices <-chan *ws.UserConn, cfg *config.Config) (*App, error) {
	app := &App{
		config:  cfg,
		orders:  orders,
		prices:  prices,
		clients: &hashmap.HashMap{},
		close:   make(chan chan (<-chan struct{}), 10),
	}

	return app, nil
}

func (a *App) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case conn := <-a.orders:
			client, err := a.getClient(ctx, conn.ID)
			if err != nil {
				conn.SendError(err)
				conn.Close()
				continue
			}

			client.SetOrdersConnection(conn)
		case conn := <-a.prices:
			client, err := a.getClient(ctx, conn.ID)
			if err != nil {
				conn.SendError(err)
				conn.Close()
				continue
			}

			client.SetPricesConnection(conn)
		}
	}
}

func (a *App) getClient(ctx context.Context, userID string) (*exchange.Client, error) {
	c, ok := a.clients.Get(userID)
	client, ok2 := c.(*exchange.Client)
	if !ok || !ok2 {
		var err error
		client, err = exchange.New(ctx, a.config)
		if err != nil {
			log.Error().Err(err).Msg("create client")

			return nil, err
		}

		log.Debug().Str("user", userID).Msg("new exchange client")
		a.clients.Set(userID, client)
	}

	return client, nil
}
