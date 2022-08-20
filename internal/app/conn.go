package app

import (
	"context"

	"github.com/phuslu/log"

	"github.com/xenking/exchange-emulator/internal/exchange"
)

type clientConn struct {
	close <-chan struct{}
	*exchange.Client
	id string
}

func (a *App) connWatcher(ctx context.Context) {
	var store []clientConn

	for {
		select {
		case <-ctx.Done():
			return
		case conn := <-a.connections:
			store = append(store, conn)
			log.Trace().Str("id", conn.id).Msg("new exchange connection")
		default:
			for i := 0; i < len(store); i++ {
				select {
				case <-ctx.Done():
					return
				case <-store[i].close:
					a.clients.Del(store[i].id)
					store[i].Close()

					log.Debug().Str("id", store[i].id).Msg("exchange closed")
					store = append(store[:i], store[i+1:]...)
				}
			}
		}
	}
}
