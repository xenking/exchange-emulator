package app

import (
	"context"

	"github.com/xenking/decimal"

	"github.com/xenking/exchange-emulator/gen/proto/api"
	"github.com/xenking/exchange-emulator/internal/balance"
	"github.com/xenking/exchange-emulator/internal/order"
	"github.com/xenking/exchange-emulator/internal/parser"
)

func (a *App) CreateOrder(ctx context.Context, userID string, apiOrder *api.Order) (*api.Order, error) {
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	var resp *api.Order
	user.NewAction(ctx, func(state parser.ExchangeState) {
		o := user.Order.Add(apiOrder, state.Unix)
		if o == nil {
			err = order.ErrNotFound
			return
		}

		err = user.UpdateBalance(o)
		resp = o.Order
	})

	return resp, err
}

func (a *App) GetOrder(ctx context.Context, userID, orderID string) (*api.Order, error) {
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	var resp *api.Order
	user.NewAction(ctx, func(state parser.ExchangeState) {
		o := user.Order.Get(orderID)
		if o == nil {
			err = order.ErrNotFound
			return
		}

		resp = o.Order
	})

	return resp, err
}

func (a *App) CancelOrder(ctx context.Context, userID, orderID string) error {
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return err
	}

	user.NewAction(ctx, func(state parser.ExchangeState) {
		o := user.Order.Delete(orderID)
		if o == nil {
			err = order.ErrNotFound
			return
		}

		err = user.UpdateBalance(o)
	})

	return err
}

func (a *App) GetBalances(ctx context.Context, userID string) (*api.Balances, error) {
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	var balances []balance.Asset
	user.NewAction(ctx, func(state parser.ExchangeState) {
		balances = user.Balance.List()
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

	return resp, nil
}

func (a *App) SetBalances(ctx context.Context, userID string, balances *api.Balances) error {
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return err
	}

	bb := make([]balance.Asset, len(balances.Data))
	for i, asset := range balances.Data {
		bb[i].Name = asset.Asset
		bb[i].Free, _ = decimal.NewFromString(asset.Free)
		bb[i].Locked, _ = decimal.NewFromString(asset.Locked)
	}

	user.NewAction(ctx, func(state parser.ExchangeState) {
		user.Balance.Set(bb)
	})

	return nil
}

func (a *App) GetPrice(ctx context.Context, userID string, symbol string) (*api.Price, error) {
	user, err := a.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	var price string
	user.NewAction(ctx, func(state parser.ExchangeState) {
		price = state.Close.String()
	})

	return &api.Price{
		Price: price,
	}, nil
}
