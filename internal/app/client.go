package app

import (
	"context"

	"github.com/xenking/decimal"

	"github.com/xenking/exchange-emulator/gen/proto/api"
	"github.com/xenking/exchange-emulator/internal/balance"
	"github.com/xenking/exchange-emulator/internal/exchange"
	"github.com/xenking/exchange-emulator/internal/order"
	"github.com/xenking/exchange-emulator/internal/parser"
)

type Client struct {
	*exchange.Client
}

func (c *Client) CreateOrder(ctx context.Context, userID string, apiOrder *api.Order) (*api.Order, error) {
	c.Log.Trace().Str("type", "create order").Msg("grpc action")

	apiOrder.UserId = userID

	var (
		err  error
		resp *api.Order
	)
	c.NewAction(ctx, func(state parser.ExchangeState) {
		o := c.Order.Add(apiOrder, state.Unix)
		if o == nil {
			err = order.ErrNotFound
			return
		}

		err = c.UpdateBalance(o)
		resp = &api.Order{
			Id:           o.Id,
			Symbol:       o.Symbol,
			Side:         o.Side,
			Type:         o.Type,
			Price:        o.Order.Price,
			Quantity:     o.Order.Quantity,
			Total:        o.Order.Total,
			Status:       o.Status,
			OrderId:      o.OrderId,
			UserId:       o.UserId,
			TransactTime: o.TransactTime,
		}
	})

	return resp, err
}

func (c *Client) ReplaceOrder(ctx context.Context, userID string, cancelID string, apiOrder *api.Order) (*api.Order, error) {
	c.Log.Trace().Str("type", "replace order").Msg("grpc action")

	apiOrder.UserId = userID

	var (
		err  error
		resp *api.Order
	)
	c.NewAction(ctx, func(state parser.ExchangeState) {
		cancel := c.Order.Cancel(cancelID)
		if cancel != nil {
			err = c.UpdateBalance(cancel)
			if err != nil {
				return
			}
		}

		o := c.Order.Add(apiOrder, state.Unix)
		if o == nil {
			err = order.ErrNotFound
			return
		}

		err = c.UpdateBalance(o)
		resp = &api.Order{
			Id:           o.Id,
			Symbol:       o.Symbol,
			Side:         o.Side,
			Type:         o.Type,
			Price:        o.Order.Price,
			Quantity:     o.Order.Quantity,
			Total:        o.Order.Total,
			Status:       o.Status,
			OrderId:      o.OrderId,
			UserId:       o.UserId,
			TransactTime: o.TransactTime,
		}
	})

	return resp, err
}

func (c *Client) CreateOrders(ctx context.Context, userID string, apiOrders []*api.Order) ([]*api.Order, error) {
	c.Log.Trace().Str("type", "create orders").Msg("grpc action")

	var err error
	resp := make([]*api.Order, len(apiOrders))
	c.NewAction(ctx, func(state parser.ExchangeState) {
		unix := state.Unix
		for i, apiOrder := range apiOrders {
			apiOrder.UserId = userID
			unix += 1 // add 10 ms time offset to prevent duplicate orders

			o := c.Order.Add(apiOrder, unix)
			if o == nil {
				err = order.ErrNotFound
				return
			}

			err = c.UpdateBalance(o)
			resp[i] = &api.Order{
				Id:           o.Id,
				Symbol:       o.Symbol,
				Side:         o.Side,
				Type:         o.Type,
				Price:        o.Order.Price,
				Quantity:     o.Order.Quantity,
				Total:        o.Order.Total,
				Status:       o.Status,
				OrderId:      o.OrderId,
				UserId:       o.UserId,
				TransactTime: o.TransactTime,
			}
		}
	})

	return resp, err
}

func (c *Client) GetOrder(ctx context.Context, orderID string) (*api.Order, error) {
	c.Log.Trace().Str("type", "get order").Msg("grpc action")
	var (
		err  error
		resp *api.Order
	)
	c.NewAction(ctx, func(state parser.ExchangeState) {
		o := c.Order.Get(orderID)
		if o == nil {
			err = order.ErrNotFound
			return
		}

		resp = &api.Order{
			Id:           o.Id,
			Symbol:       o.Symbol,
			Side:         o.Side,
			Type:         o.Type,
			Price:        o.Order.Price,
			Quantity:     o.Order.Quantity,
			Total:        o.Order.Total,
			Status:       o.Status,
			OrderId:      o.OrderId,
			UserId:       o.UserId,
			TransactTime: o.TransactTime,
		}
	})

	return resp, err
}

func (c *Client) CancelOrder(ctx context.Context, orderID string) error {
	c.Log.Trace().Str("type", "cancel order").Msg("grpc action")
	var err error
	c.NewAction(ctx, func(state parser.ExchangeState) {
		o := c.Order.Cancel(orderID)
		if o == nil {
			err = order.ErrNotFound
			return
		}

		err = c.UpdateBalance(o)
	})

	return err
}

func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	c.Log.Trace().Str("type", "cancel orders").Msg("grpc action")
	var err error
	c.NewAction(ctx, func(state parser.ExchangeState) {
		for _, orderID := range orderIDs {
			c.Log.Trace().Str("id", orderID).Msg("cancelling order")

			o := c.Order.Cancel(orderID)
			if o == nil {
				err = order.ErrNotFound
				return
			}

			err = c.UpdateBalance(o)
		}
	})

	return err
}

func (c *Client) GetBalances(ctx context.Context) *api.Balances {
	c.Log.Trace().Str("type", "get balances").Msg("grpc action")
	var balances []balance.Asset
	c.NewAction(ctx, func(state parser.ExchangeState) {
		balances = c.Balance.List()
	})

	resp := &api.Balances{
		Data: make([]*api.Balance, len(balances)),
	}
	for i, asset := range balances {
		resp.Data[i] = &api.Balance{
			Asset:  asset.Name,
			Free:   asset.Free.String(),
			Locked: asset.Locked.String(),
		}
	}

	return resp
}

func (c *Client) SetBalances(ctx context.Context, balances *api.Balances) {
	c.Log.Trace().Str("type", "set balances").Msg("grpc action")
	bb := make([]balance.Asset, len(balances.Data))
	for i, asset := range balances.Data {
		bb[i].Name = asset.Asset
		bb[i].Free, _ = decimal.NewFromString(asset.Free)
		bb[i].Locked, _ = decimal.NewFromString(asset.Locked)
	}

	c.NewAction(ctx, func(state parser.ExchangeState) {
		c.Balance.Set(bb)
	})
}

func (c *Client) GetPrice(ctx context.Context, symbol string) *api.Price {
	c.Log.Trace().Str("type", "get price").Msg("grpc action")
	var price string
	c.NewAction(ctx, func(state parser.ExchangeState) {
		price = state.Close.String()
	})

	return &api.Price{
		Price: price,
	}
}
