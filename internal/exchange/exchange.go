package exchange

import (
	"context"
	"sync/atomic"

	"github.com/phuslu/log"
	"github.com/xenking/bytebufferpool"
	"github.com/xenking/decimal"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/gen/proto/api"
	"github.com/xenking/exchange-emulator/internal/balance"
	"github.com/xenking/exchange-emulator/internal/order"
	"github.com/xenking/exchange-emulator/internal/parser"
	"github.com/xenking/exchange-emulator/internal/ws"
)

type Client struct {
	Parser        *parser.Listener
	Balance       *balance.Tracker
	Order         *order.Tracker
	Log           *log.Logger
	orderConn     *ws.UserConn
	priceConn     *ws.UserConn
	actions       chan Action
	shutdown      chan struct{}
	cancel        context.CancelFunc
	cancelHandler func(state parser.ExchangeState)
	commission    decimal.Decimal
	closed        int32
}

type Action func(parser.ExchangeState)

func New(parentCtx context.Context, config *config.Config, listener *parser.Listener, logger *log.Logger) *Client {
	ctx, cancel := context.WithCancel(parentCtx)

	b := balance.New()
	b.SetLogger(logger)
	go b.Start(ctx)

	o := order.New()
	o.SetLogger(logger)
	go o.Start(ctx)

	commission := decimal.NewFromFloat(config.Exchange.Commission).Shift(-2)
	commission = one.Sub(commission)

	ex := &Client{
		Parser:     listener,
		Balance:    b,
		Order:      o,
		Log:        logger,
		actions:    make(chan Action, 1024),
		shutdown:   make(chan struct{}),
		cancel:     cancel,
		commission: commission,
	}

	go ex.Start(ctx)

	return ex
}

func (c *Client) Shutdown() <-chan struct{} {
	return c.shutdown
}

func (c *Client) Start(ctx context.Context) {
	defer close(c.actions)
	defer close(c.shutdown)
	defer c.Close()

	states := c.Parser.ExchangeStates()
	state, opened := <-states
	if !opened {
		c.Log.Warn().Msg("exchange closed")
		return
	}

	var currentStates <-chan parser.ExchangeState
	var deletedOrders []string
	var lastState parser.ExchangeState

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.orderConn.Shutdown():
			c.Log.Warn().Msg("orders connection closed")
			return
		case <-c.priceConn.Shutdown():
			c.Log.Warn().Msg("prices connection closed")
			return
		case <-c.Order.Control():
			if currentStates != nil {
				c.Log.Debug().Msg("stop exchange")
				currentStates = nil
			} else {
				c.Log.Debug().Msg("start exchange")
				currentStates = states
			}
		case act := <-c.actions:
			state.Unix += 1 // add 1 ms time offset to prevent duplicate orders
			c.Log.Trace().Int64("ts", state.Unix).Msg("exchange action")
			act(state)

			nextAction := true
			for nextAction {
				select {
				case <-ctx.Done():
					return
				case act, nextAction = <-c.actions:
					if !nextAction {
						c.Log.Warn().Msg("actions closed")
						break
					}
					state.Unix += 1 // add 10 ms time offset to prevent duplicate orders
					c.Log.Trace().Int64("ts", state.Unix).Msg("exchange action")
					act(state)
				default:
					nextAction = false
				}
			}
		case state, opened = <-currentStates:
			if !opened {
				c.Log.Warn().Msg("exchange closed")
				currentStates = nil
				if c.cancelHandler != nil {
					c.cancelHandler(lastState)
				}
				return
			}
			lastState = state

			c.Log.Trace().Int64("ts", state.Unix).Msg("exchange state")

			if err := c.priceConn.Send(state.Raw); err != nil {
				c.Log.Error().Err(err).Str("user", c.priceConn.ID).Msg("can't send price state")
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
					// TODO: skip for now because it's never happens before
					//if o.Total.GreaterThan(state.AssetVolume) {
					//	c.Log.Panic().Str("side", o.Side.String()).Str("total", o.Total.String()).
					//		Str("asset", state.AssetVolume.String()).
					//		Int64("ts", state.Unix).Msg("can't close order in one kline. Need to use PARTIAL_FILLED")
					//}

					o.Status = api.OrderStatus_FILLED

					c.Log.Debug().Str("order", o.Id).Uint64("internal", o.OrderId).Str("user", o.UserId).Str("symbol", o.Symbol).
						Str("side", o.Side.String()).Str("price", o.Order.Price).Str("qty", o.Order.Quantity).Int64("ts", state.Unix).
						Msg("order filled")

					if err := c.UpdateBalance(o); err != nil {
						c.Log.Error().Err(err).Str("user", o.UserId).Str("order", o.Id).Msg("can't update balance")
						continue
					}

					deletedOrders = append(deletedOrders, o.Id)

					buf := bytebufferpool.GetLen(29)
					buf.B = buf.B[:0]
					buf.B = o.AppendEncoded(buf.B)
					err := c.orderConn.Send(buf.B)
					bytebufferpool.Put(buf)
					if err != nil {
						c.Log.Error().Err(err).Str("user", c.orderConn.ID).Str("order", o.Id).Msg("can't send order update")
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
		go c.listenWSClose(conn)
	}
}

func (c *Client) SetPricesConnection(conn *ws.UserConn) {
	c.actions <- func(state parser.ExchangeState) {
		if c.priceConn != nil {
			c.priceConn.Close()
		}

		c.priceConn = conn
		go c.listenWSClose(conn)
	}
}

func (c *Client) SetCancelHandler(handler func(state parser.ExchangeState)) {
	c.actions <- func(state parser.ExchangeState) {
		c.cancelHandler = handler
	}
}

func (c *Client) Close() {
	if atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		c.cancel()
		c.Parser.Close()
		c.orderConn.Close()
		c.priceConn.Close()
	}
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

var one = decimal.NewFromInt(1)

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
		c.Log.Trace().Str("side", o.Side.String()).Str("asset", asset.Name).
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
		c.Log.Trace().Str("side", o.Side.String()).Str("asset", asset.Name).
			Str("free", asset.Free.String()).Str("locked", asset.Locked.String()).
			Str("order", o.Id).Str("status", o.Status.String()).Msg("balance update finished 1")

		if asset.Free.IsNegative() || asset.Locked.IsNegative() {
			c.Log.Error().Str("asset", asset.Name).
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
			c.Log.Trace().Str("side", o.Side.String()).Str("asset", asset.Name).
				Str("free", asset.Free.String()).Str("locked", asset.Locked.String()).
				Str("order", o.Id).Str("status", o.Status.String()).Msg("balance update started 2")
			switch o.GetSide() {
			case api.OrderSide_BUY:
				if c.commission.IsZero() {
					asset.Free = asset.Free.Add(o.Quantity)
				} else {
					asset.Free = asset.Free.Add(o.Quantity.Mul(c.commission))
				}
			case api.OrderSide_SELL:
				if c.commission.IsZero() {
					asset.Free = asset.Free.Add(o.Total)
				} else {
					asset.Free = asset.Free.Add(o.Total.Mul(c.commission))
				}
			}

			if asset.Free.IsNegative() || asset.Locked.IsNegative() {
				c.Log.Error().Str("asset", asset.Name).
					Str("free", asset.Free.String()).
					Str("locked", asset.Locked.String()).
					Str("total", o.Total.String()).Msg("balance update failed")

				return balance.ErrNegative
			}

			c.Log.Trace().Str("side", o.Side.String()).Str("asset", asset.Name).
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

func (c *Client) listenWSClose(conn *ws.UserConn) {
	select {
	case <-c.shutdown:
	case <-conn.Shutdown():
	}

	c.Close()
	return
}
