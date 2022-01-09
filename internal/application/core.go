package application

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/cornelk/hashmap"
	"github.com/phuslu/log"
	"github.com/pkg/errors"
	"github.com/segmentio/encoding/json"
	"github.com/xenking/decimal"

	"github.com/xenking/exchange-emulator/models"
)

type Core struct {
	UserBalances *hashmap.HashMap // map[userID][]*Balance
	Exchanges    *hashmap.HashMap // map[userID]*Exchange
	Orders       *hashmap.HashMap // map[OrderIDReq]*Order
	callback     func(order *models.Order)

	commission    decimal.Decimal
	exchangeFile  string
	orderSequence uint64
}

func NewCore(exchangeFile string, commission decimal.Decimal) *Core {
	return &Core{
		UserBalances: &hashmap.HashMap{},
		Orders:       &hashmap.HashMap{},
		Exchanges:    &hashmap.HashMap{},

		exchangeFile: exchangeFile,
		commission:   commission,
	}
}

func (c *Core) SetCallback(cb func(order *models.Order)) {
	c.callback = cb
}

func (c *Core) AddExchange(ctx context.Context, user uint64, file string) error {
	exchange, err := NewExchange(ctx, file, c.updateState)
	if err != nil {
		return err
	}
	c.Exchanges.Set(user, exchange)

	return nil
}

func (c *Core) CurrState(user uint64) *models.ExchangeState {
	ex, ok := c.Exchanges.Get(user)
	if !ok {
		return nil
	}
	exchange, ok2 := ex.(*Exchange)
	if !ok2 {
		return nil
	}

	var state *models.ExchangeState
	wait := make(chan struct{})
	exchange.transactions <- func(s *models.ExchangeState) bool {
		state = s
		close(wait)

		return true
	}
	<-wait

	return state
}

func (c *Core) ExchangeInfo(w io.Writer) error {
	buf, err := LoadExchangeInfo(c.exchangeFile)
	if err != nil {
		return err
	}
	_, _ = w.Write(buf)

	return nil
}

var (
	ErrEmptySymbol     = errors.New("empty symbol")
	ErrNoData          = errors.New("no data")
	ErrUnknownUser     = errors.New("unknown user")
	ErrEmptyBalance    = errors.New("empty balance")
	ErrNegativeBalance = errors.New("negative balance")
)

func (c *Core) GetPrice(w io.Writer, user uint64, data []byte) error {
	req := &models.PriceReq{}
	if err := json.Unmarshal(data, req); err != nil {
		return err
	}
	if req.Symbol == "" {
		return ErrEmptySymbol
	}
	state := c.CurrState(user)
	if state == nil {
		return ErrNoData
	}
	req.Price = state.Close
	req.Op = 0
	log.Debug().Uint64("user", user).Str("symbol", req.Symbol).Float64("price", req.Price.InexactFloat64()).Msg("")
	err := json.NewEncoder(w).Encode(req)

	return err
}

func (c *Core) SetBalance(w io.Writer, user uint64, data []byte) error {
	req := &models.BalancesReq{}
	if err := json.Unmarshal(data, req); err != nil {
		return err
	}
	c.UserBalances.Set(user, req.Balances)
	req.Op = 0
	log.Debug().Uint64("user", user).RawJSON("data", data).Msg("Balance set")
	err := json.NewEncoder(w).Encode(req)

	return err
}

func (c *Core) GetBalance(w io.Writer, user uint64) error {
	b, ok := c.UserBalances.Get(user)
	if !ok {
		return ErrUnknownUser
	}
	err := json.NewEncoder(w).Encode(b)

	return err
}

func (c *Core) CreateOrder(w io.Writer, user uint64, data []byte) error {
	order := &models.Order{}
	if err := json.Unmarshal(data, order); err != nil {
		return err
	}
	order.UserID = user
	order.OrderID = atomic.AddUint64(&c.orderSequence, 1)
	order.Status = models.OrderStatusNew
	if order.Side == models.OrderSideBuy {
		order.Total = order.Price.Mul(order.Quantity)
	} else {
		order.Total = order.Quantity.Div(order.Price)
	}
	order.Operation.Op = 0

	if err := c.updateBalance(order); err != nil {
		return err
	}
	c.Orders.Set(order.ID, order)
	log.Debug().Str("order", order.ID).Uint64("user", order.UserID).Str("symbol", order.Symbol).
		Str("side", order.Side).Msg("Order created")
	err := json.NewEncoder(w).Encode(order)

	return err
}

func (c *Core) GetOrder(w io.Writer, data []byte) error {
	o := &models.OrderIDReq{}
	if err := json.Unmarshal(data, o); err != nil {
		return err
	}
	v, ok := c.Orders.Get(o.ID)
	if !ok {
		return ErrNoData
	}
	err := json.NewEncoder(w).Encode(v)

	return err
}

func (c *Core) CancelOrder(w io.Writer, data []byte) error {
	req := &models.OrderIDReq{}
	if err := json.Unmarshal(data, req); err != nil {
		return err
	}
	o, ok := c.Orders.Get(req.ID)
	if !ok {
		return ErrNoData
	}
	order, ok2 := o.(*models.Order)
	if !ok2 {
		return ErrNoData
	}
	c.Orders.Del(order.Price)
	order.Status = models.OrderStatusCanceled

	if err := c.updateBalance(order); err != nil {
		return err
	}
	log.Debug().Str("order", order.ID).Uint64("user", order.UserID).Str("symbol", order.Symbol).
		Str("side", order.Side).Msg("Order canceled")
	err := json.NewEncoder(w).Encode(order)

	return err
}

func (c *Core) updateState(state *models.ExchangeState) (updated bool) {
	for kv := range c.Orders.Iter() {
		order, ok := kv.Value.(*models.Order)
		if !ok {
			continue
		}
		if order.Price.LessThan(state.Low) || order.Price.GreaterThan(state.High) {
			continue
		}
		if order.Side == models.OrderSideBuy && order.Total.GreaterThan(state.AssetVolume) ||
			order.Side == models.OrderSideSell && order.Total.GreaterThan(state.BaseVolume) {
			log.Panic().Str("side", order.Side).Str("total", order.Total.String()).
				Str("asset", state.AssetVolume.String()).Str("base", state.BaseVolume.String()).
				Int64("ts", state.Unix).Msg("can't close order in one kline. Need to use PARTIAL_FILLED")
		}

		updated = true
		order.Status = models.OrderStatusFilled
		log.Debug().Str("order", order.ID).Uint64("user", order.UserID).Str("symbol", order.Symbol).
			Str("side", order.Side).Msg("Order filled")
		if err := c.updateBalance(order); err != nil {
			log.Error().Err(err).Str("order", order.ID).Msg("can't update balance")

			continue
		}
		if c.callback != nil {
			c.callback(order)
		}
		c.Orders.Del(order.ID)
	}

	return updated
}

var zero = decimal.NewFromInt(0)

func (c *Core) updateBalance(o *models.Order) error {
	bb, ok := c.UserBalances.Get(o.UserID)
	if !ok {
		return ErrEmptyBalance
	}
	balances, ok2 := bb.([]*models.Balance)
	if !ok2 {
		return ErrEmptyBalance
	}
	var base, quote string
	if o.Side == models.OrderSideBuy {
		base, quote = o.Symbol[3:], o.Symbol[:3]
	} else {
		base, quote = o.Symbol[:3], o.Symbol[3:]
	}
	var input, output *models.Balance
	for _, b := range balances {
		switch b.Asset {
		case base:
			input = b
		case quote:
			output = b
		}
	}
	if input == nil {
		input = &models.Balance{
			Asset:  base,
			Free:   zero,
			Locked: zero,
		}
		balances = append(balances, input)
		c.UserBalances.Set(o.UserID, balances)
	}
	if output == nil {
		output = &models.Balance{
			Asset:  quote,
			Free:   zero,
			Locked: zero,
		}
		balances = append(balances, output)
		c.UserBalances.Set(o.UserID, balances)
	}

	switch o.Status {
	case models.OrderStatusNew:
		input.Free = input.Free.Sub(o.Total)
		input.Locked = input.Locked.Add(o.Total)
		if input.Free.IsNegative() {
			return ErrNegativeBalance
		}
	case models.OrderStatusCanceled:
		input.Locked = input.Locked.Sub(o.Total)
		input.Free = input.Free.Add(o.Total)
	case models.OrderStatusFilled:
		input.Locked = input.Locked.Sub(o.Total)
		output.Free = output.Free.Add(o.Quantity.Sub(o.Quantity.Mul(c.commission)))
		if input.Locked.IsNegative() {
			return ErrNegativeBalance
		}
	}
	log.Info().Uint64("user", o.UserID).
		Str("base_asset", input.Asset).Float64("base_free", input.Free.InexactFloat64()).Float64("base_locked", input.Locked.InexactFloat64()).
		Str("quote_asset", output.Asset).Float64("quote_free", output.Free.InexactFloat64()).Float64("quote_locked", output.Locked.InexactFloat64()).
		Msg("Balance updated")

	return nil
}

func (c *Core) UserExchange(user uint64) *Exchange {
	ex, ok := c.Exchanges.Get(user)
	if !ok {
		return nil
	}
	exchange, ok2 := ex.(*Exchange)
	if !ok2 {
		return nil
	}

	return exchange
}

func (c *Core) DeleteExchange(user uint64) {
	c.UserBalances.Del(user)

	ex, ok := c.Exchanges.Get(user)
	if !ok {
		return
	}
	exchange, ok2 := ex.(*Exchange)
	if !ok2 {
		return
	}
	exchange.Close()
	log.Debug().Uint64("user", user).Msg("Exchange deleted")
}
