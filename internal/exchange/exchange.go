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
	Parser  *parser.Listener
	Balance *balance.Tracker
	Order   *order.Tracker

	orderConn *ws.UserConn
	priceConn *ws.UserConn

	commission decimal.Decimal
	actions    chan Action
	cancel     context.CancelFunc
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
		actions:    make(chan Action, 200),
		cancel:     cancel,
		commission: decimal.NewFromFloat(config.Exchange.Commission),
	}

	go ex.Start(ctx)

	return ex, err
}

func (c *Client) Start(ctx context.Context) {
	defer c.Close()

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
		case <-c.orderConn.CloseHandler():
			log.Warn().Msg("orders connection closed")
			return
		case <-c.priceConn.CloseHandler():
			log.Warn().Msg("prices connection closed")
			return
		case <-c.Order.Control():
			if currentStates != nil {
				log.Debug().Msg("stop exchange")
				currentStates = nil
			} else {
				log.Debug().Msg("start exchange")
				currentStates = states
			}
		case act := <-c.actions:
			state.Unix += 10 // add 10 ms time offset to prevent duplicate orders
			log.Trace().Int64("ts", state.Unix).Msg("exchange action")
			act(state)

			nextAction := true
			for nextAction {
				select {
				case <-ctx.Done():
					return
				case act, nextAction = <-c.actions:
					if !nextAction {
						log.Warn().Msg("actions closed")
						break
					}
					state.Unix += 10 // add 10 ms time offset to prevent duplicate orders
					log.Trace().Int64("ts", state.Unix).Msg("exchange action")
					act(state)
				default:
					nextAction = false
				}
			}
		case state, opened = <-currentStates:
			if !opened {
				log.Warn().Msg("exchange closed")
				currentStates = nil
				return
			}

			log.Trace().Int64("ts", state.Unix).Msg("exchange state")

			if err := c.priceConn.Send(state); err != nil {
				log.Error().Err(err).Str("user", c.priceConn.ID).Msg("can't send price state")
				continue
			}

			deletedOrders = deletedOrders[:0]

			c.Order.Range(func(orders []*order.Order) {
				for _, o := range orders {
					// buy
					// price=10, high=12, low=9 -> bought
					// price=10, high=15, low=11 -> continue
					// price=10, high=9, low=8 -> bought
					// price >= low
					// sell
					// price=10, high=12, low=9 -> sold
					// price=10, high=15, low=11 -> sold
					// price=10, high=9, low=8 -> continue
					// price <= high
					if o.Side == api.OrderSide_BUY && o.Price.LessThan(state.Low) ||
						o.Side == api.OrderSide_SELL && o.Price.GreaterThan(state.High) {
						continue
					}
					if o.Total.GreaterThan(state.AssetVolume) {
						log.Panic().Str("side", o.Side.String()).Str("total", o.Total.String()).
							Str("asset", state.AssetVolume.String()).
							Int64("ts", state.Unix).Msg("can't close order in one kline. Need to use PARTIAL_FILLED")
					}

					o.Status = api.OrderStatus_FILLED

					log.Debug().Str("order", o.Id).Str("user", o.UserId).Str("symbol", o.Symbol).
						Str("side", o.Side.String()).Str("price", o.Order.Price).Str("qty", o.Order.Quantity).
						Msg("order filled on exchange")

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

			switch len(deletedOrders) {
			case 0:
				continue
			case 1:
				c.Order.Cancel(deletedOrders[0])
			default:
				c.Order.RemoveRange(deletedOrders)
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
	c.cancel()
	close(c.actions)
	c.orderConn.Close()
	c.priceConn.Close()
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

var (
	one     = decimal.NewFromInt(1)
	percent = decimal.NewFromInt(100)
)

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
		log.Trace().Str("side", o.Side.String()).Str("asset", asset.Name).
			Str("free", asset.Free.String()).Str("locked", asset.Locked.String()).
			Str("order", o.Id).Str("status", o.Status.String()).Msg("balance update started 1")
		switch o.GetStatus() {
		case api.OrderStatus_NEW:
			switch o.GetSide() {
			case api.OrderSide_BUY:
				asset.Free = asset.Free.Sub(o.Total)
				asset.Locked = asset.Locked.Add(o.Total)
			case api.OrderSide_SELL:
				asset.Free = asset.Free.Sub(o.Quantity)
				asset.Locked = asset.Locked.Add(o.Quantity)
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
				asset.Locked = asset.Locked.Sub(o.Quantity)
				asset.Free = asset.Free.Add(o.Quantity)
			}
		}
		log.Trace().Str("side", o.Side.String()).Str("asset", asset.Name).
			Str("free", asset.Free.String()).Str("locked", asset.Locked.String()).
			Str("order", o.Id).Str("status", o.Status.String()).Msg("balance update finished 1")

		if asset.Free.IsNegative() || asset.Locked.IsNegative() {
			log.Error().Str("asset", asset.Name).
				Str("free", asset.Free.String()).
				Str("locked", asset.Locked.String()).
				Str("total", o.Total.String()).Msg("balance update failed")

			return balance.ErrNegative
		}
		return
	})
	if err != nil {
		return err
	}
	if o.Status == api.OrderStatus_FILLED {
		err = c.Balance.NewTransaction(quote, func(asset *balance.Asset) (err error) {
			log.Trace().Str("side", o.Side.String()).Str("asset", asset.Name).
				Str("free", asset.Free.String()).Str("locked", asset.Locked.String()).
				Str("order", o.Id).Str("status", o.Status.String()).Msg("balance update started 2")
			switch o.GetSide() {
			case api.OrderSide_BUY:
				asset.Free = asset.Free.Add(o.Quantity.Mul(c.commission.Div(percent).Neg().Add(one)))
			case api.OrderSide_SELL:
				asset.Free = asset.Free.Add(o.Total.Mul(c.commission.Div(percent).Neg().Add(one)))
			}

			if asset.Free.IsNegative() || asset.Locked.IsNegative() {
				log.Error().Str("asset", asset.Name).
					Str("free", asset.Free.String()).
					Str("locked", asset.Locked.String()).
					Str("total", o.Total.String()).Msg("balance update failed")

				return balance.ErrNegative
			}

			log.Trace().Str("side", o.Side.String()).Str("asset", asset.Name).
				Str("free", asset.Free.String()).Str("locked", asset.Locked.String()).
				Str("order", o.Id).Str("status", o.Status.String()).Msg("balance update finished 2")
			return
		})
		if err != nil {
			return err
		}
	}

	return nil
}
