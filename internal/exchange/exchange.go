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
	orderConn *ws.UserConn
	priceConn *ws.UserConn

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
		actions:    make(chan Action, 100),
		close:      cancel,
		commission: decimal.NewFromFloat(config.Exchange.Commission),
	}

	go ex.Start(ctx)

	return ex, err
}

func (c *Client) Start(ctx context.Context) {
	states := c.Parser.ExchangeStates()

	state, opened := <-states
	if !opened {
		log.Warn().Msg("exchange closed")
		return
	}

	var currentStates <-chan parser.ExchangeState
	var deletedOrders []string

	for {
		select {
		case <-ctx.Done():
			return
		case act := <-c.actions:
			state.Unix += 100 // add 100 ms time offset to prevent duplicate orders
			log.Trace().Int64("ts", state.Unix).Msg("exchange action")
			act(state)
		case <-c.Order.Control():
			if currentStates != nil {
				log.Debug().Msg("stop exchange")
				currentStates = nil
			} else {
				log.Debug().Msg("start exchange")
				currentStates = states
			}
		case state, opened = <-currentStates:
			if !opened {
				log.Warn().Msg("exchange closed")
				currentStates = nil
			}

			log.Trace().Int64("ts", state.Unix).Msg("exchange state")

			if err := c.priceConn.Send(state); err != nil {
				log.Error().Err(err).Str("user", c.priceConn.ID).Msg("can't send price state")
				continue
			}

			deletedOrders = deletedOrders[:0]

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

					log.Debug().Str("order", o.Id).Str("user", o.UserId).Str("symbol", o.Symbol).
						Str("side", o.Side.String()).Msg("exchange: order filled")

					if err := c.UpdateBalance(o); err != nil {
						log.Error().Err(err).Str("user", o.UserId).Str("order", o.Id).Msg("can't update balance")
						continue
					}

					deletedOrders = append(deletedOrders, o.Id)

					if err := c.orderConn.Send(o.Order); err != nil {
						log.Error().Err(err).Str("user", c.orderConn.ID).Str("order", o.Id).Msg("can't send order update")
						continue
					}
				}
			})

			for _, o := range deletedOrders {
				c.Order.Delete(o)
			}
		}
	}
}

func (c *Client) SetOrdersConnection(conn *ws.UserConn) {
	c.actions <- func(state parser.ExchangeState) {
		if c.orderConn != nil {
			c.orderConn.Close()
		}

		c.orderConn = conn
	}
}

func (c *Client) SetPricesConnection(conn *ws.UserConn) {
	c.actions <- func(state parser.ExchangeState) {
		if c.priceConn != nil {
			c.priceConn.Close()
		}

		c.priceConn = conn
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

// UpdateBalance updates user balance for order
// pair USDT ETH
// NEW order
// 1. BUY:  USDT free-total
// 2. BUY:  USDT locked+total
// 1. SELL: ETH  free-quantity
// 2. SELL: ETH  locked+quantity
// CANCEL order
// 1. BUY:  USDT locked-total
// 2. BUY:  USDT free+total
// 1. SELL: ETH  locked-quantity
// 2. SELL: ETH  free+quantity
// FILL order
// 1. BUY:  USDT locked-total
// 2. BUY:  ETH  free+quantity
// 1. SELL: ETH  locked-quantity
// 2. SELL: USDT free+total
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

		if asset.Free.IsNegative() || asset.Locked.IsNegative() {
			log.Error().Str("asset", asset.Name).
				Str("free", asset.Free.String()).
				Str("locked", asset.Locked.String()).
				Str("total", o.Total.String()).Msg("new order")

			return balance.ErrNegative
		}

		log.Trace().Str("user", o.UserId).
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

			if asset.Free.IsNegative() || asset.Locked.IsNegative() {
				log.Error().Str("asset", asset.Name).
					Str("free", asset.Free.String()).
					Str("locked", asset.Locked.String()).
					Str("total", o.Total.String()).Msg("new order")

				return balance.ErrNegative
			}

			log.Debug().Str("user", o.UserId).
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
