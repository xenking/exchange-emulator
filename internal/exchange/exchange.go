package exchange

import (
	"context"
	"github.com/phuslu/log"
	"github.com/xenking/decimal"
	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/gen/proto/api"
	"github.com/xenking/exchange-emulator/internal/balance"
	"github.com/xenking/exchange-emulator/internal/order"
	"github.com/xenking/exchange-emulator/internal/parser"
	"github.com/xenking/exchange-emulator/internal/ws"
)

type Client struct {
	OrderConn *ws.UserConn
	PriceConn *ws.UserConn

	Parser  *parser.Listener
	Balance *balance.Tracker
	Order   *order.Tracker

	commission decimal.Decimal
	actions    chan Action
	close      context.CancelFunc
}

type Action func(parser.ExchangeState)

func New(parentCtx context.Context, config *config.Config) (*Client, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	p, err := parser.New(ctx, config.Parser)
	if err != nil {
		cancel()

		return nil, err
	}

	b := balance.New()
	go b.Start(ctx)

	o := order.New()
	go o.Start(ctx)

	ex := &Client{
		Parser:     p,
		Balance:    b,
		Order:      o,
		actions:    make(chan Action, 10),
		close:      cancel,
		commission: decimal.NewFromFloat(config.Exchange.Commission),
	}

	go ex.Start(ctx)

	return ex, err
}

func (c *Client) Start(ctx context.Context) {
	states := c.Parser.ExchangeStates()

	var opened bool
	var state parser.ExchangeState

	for {
		select {
		case <-ctx.Done():
			return
		case act := <-c.actions:
			act(state)
		case state, opened = <-states:
			if !opened {
				log.Warn().Msg("exchange closed")
				return
			}

			if err := c.PriceConn.Send(state); err != nil {
				log.Error().Err(err).Str("user", c.PriceConn.ID).Msg("can't send price state")
				return
			}

			c.Order.Range(func(orders []*order.Order) {
				for _, o := range orders {
					if o.Price.LessThan(state.Low) || o.Price.GreaterThan(state.High) {
						continue
					}
					if o.Total.GreaterThan(state.AssetVolume) {
						log.Panic().Str("side", o.Side.String()).Str("total", o.Total.String()).
							Str("asset", state.AssetVolume.String()).
							Int64("ts", state.Unix).Msg("can't close order in one kline. Need to use PARTIAL_FILLED")
					}

					o.Status = api.OrderStatus_FILLED

					if err := c.UpdateBalance(o); err != nil {
						log.Error().Err(err).Str("user", o.UserId).Str("order", o.Id).Msg("can't update balance")

						return
					}

					if err := c.OrderConn.Send(o.Order); err != nil {
						log.Error().Err(err).Str("user", c.OrderConn.ID).Str("order", o.Id).Msg("can't send order update")

						return
					}
				}
			})
		}
	}
}

func (c *Client) Close() {
	c.close()
	close(c.actions)
}

func (c *Client) NewAction(ctx context.Context, action Action) {
	done := make(chan struct{})
	select {
	case <-ctx.Done():
		return
	case c.actions <- func(state parser.ExchangeState) {
		action(state)
		close(done)
	}:
	}
	<-done
}

func (c *Client) UpdateBalance(o *order.Order) error {
	var base, quote string
	if o.GetSide() == api.OrderSide_BUY {
		base, quote = o.Symbol[3:], o.Symbol[:3]
	} else {
		base, quote = o.Symbol[:3], o.Symbol[3:]
	}

	err := c.Balance.NewTransaction(base, func(asset *balance.Asset) (err error) {
		switch o.GetStatus() {
		case api.OrderStatus_NEW:
			switch o.GetSide() {
			case api.OrderSide_BUY:
				asset.Free = asset.Free.Sub(o.Total)
				asset.Locked = asset.Locked.Add(o.Total)
			case api.OrderSide_SELL:
				asset.Free.Sub(o.Quantity)
				asset.Locked.Add(o.Quantity)
			}
		case api.OrderStatus_FILLED:
			switch o.GetSide() {
			case api.OrderSide_BUY:
				asset.Locked = asset.Locked.Sub(o.Total)
			case api.OrderSide_SELL:
				asset.Locked = asset.Locked.Sub(o.Quantity)
			}
		case api.OrderStatus_CANCELED:
			switch o.GetSide() {
			case api.OrderSide_BUY:
				asset.Locked = asset.Locked.Sub(o.Total)
				asset.Free = asset.Free.Add(o.Total)
			case api.OrderSide_SELL:
				asset.Locked.Sub(o.Quantity)
				asset.Free.Add(o.Quantity)
			}
		}

		if asset.Free.IsNegative() {
			log.Error().Str("asset", asset.Name).
				Str("free", asset.Free.String()).
				Str("locked", asset.Locked.String()).
				Str("total", o.Total.String()).Msg("new order")

			return balance.ErrNegative
		}
		log.Info().Str("user", o.UserId).
			Str("asset", asset.Name).
			Str("free", asset.Free.String()).
			Str("locked", asset.Locked.String()).
			Msg("balance updated")
		return
	})
	if err != nil {
		return err
	}

	if o.Status == api.OrderStatus_FILLED {
		err = c.Balance.NewTransaction(quote, func(asset *balance.Asset) (err error) {
			switch o.GetSide() {
			case api.OrderSide_BUY:
				asset.Free = asset.Free.Add(o.Quantity.Sub(o.Quantity.Mul(c.commission)))
			case api.OrderSide_SELL:
				asset.Free = asset.Free.Add(o.Total.Sub(o.Total.Mul(c.commission)))
			}
			log.Info().Str("user", o.UserId).
				Str("asset", asset.Name).
				Str("free", asset.Free.String()).
				Str("locked", asset.Locked.String()).
				Msg("balance updated")
			return
		})
		if err != nil {
			return err
		}
	}

	return nil
}
