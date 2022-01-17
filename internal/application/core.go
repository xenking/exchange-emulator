package application

import (
	"context"
	"sync/atomic"

	"github.com/cornelk/hashmap"
	"github.com/go-faster/errors"
	"github.com/phuslu/log"
	"github.com/xenking/decimal"

	"github.com/xenking/exchange-emulator/gen/proto/api"
)

var (
	ErrNoData          = errors.New("no data")
	ErrUnknownUser     = errors.New("unknown user")
	ErrEmptyBalance    = errors.New("empty balance")
	ErrNegativeBalance = errors.New("negative balance")
)

type Core struct {
	balances *hashmap.HashMap // map[userID][]*Balance
	orders   *hashmap.HashMap // map[OrderIDReq]*Order
	exchange *Exchange
	callback func(order *api.Order)

	commission    decimal.Decimal
	exchangeFile  string
	orderSequence uint64
}

func NewCore(exchangeFile string, commission decimal.Decimal) *Core {
	return &Core{
		balances: &hashmap.HashMap{},
		orders:   &hashmap.HashMap{},

		exchangeFile: exchangeFile,
		commission:   commission,
	}
}

func (c *Core) OnOrderUpdate(cb func(order *api.Order)) {
	c.callback = cb
}

func (c *Core) ExchangeInfo() (map[string]interface{}, error) {
	buf, err := LoadExchangeInfo(c.exchangeFile)

	return buf, err
}

func (c *Core) GetPrice(symbol string) (decimal.Decimal, error) {
	state := c.CurrState()
	if state == nil {
		return decimal.Decimal{}, ErrNoData
	}

	log.Debug().Str("symbol", symbol).Float64("price", state.Close.InexactFloat64()).
		Msg("Get price")

	return state.Close, nil
}

func (c *Core) CurrState() *ExchangeState {
	var state *ExchangeState
	wait := make(chan struct{})
	c.exchange.transactions <- func(s *ExchangeState) bool {
		state = s
		close(wait)

		return true
	}
	<-wait

	return state
}

func (c *Core) GetBalance(user string) (*api.Balances, error) {
	bb, ok := c.balances.Get(user)
	if !ok {
		return nil, ErrUnknownUser
	}
	balances, ok := bb.(*api.Balances)
	if !ok {
		return nil, ErrUnknownUser
	}

	return balances, nil
}

func (c *Core) SetBalance(user string, balances *api.Balances) {
	c.balances.Set(user, balances)
	log.Debug().Str("user", user).Msg("Balance set")
}

func (c *Core) CreateOrder(user string, order *api.Order) (*api.Order, error) {
	order.OrderId = atomic.AddUint64(&c.orderSequence, 1)
	order.Status = api.OrderStatus_NEW
	order.UserId = user

	price, err := decimal.NewFromString(order.GetPrice())
	if err != nil {
		return nil, err
	}
	qty, err := decimal.NewFromString(order.GetQuantity())
	if err != nil {
		return nil, err
	}
	var total decimal.Decimal
	if order.Side == api.OrderSide_BUY {
		total = price.Mul(qty)
	} else {
		total = qty.Div(price)
	}
	order.Total = total.String()

	err = c.updateBalance(order)
	if err != nil {
		return nil, err
	}
	c.orders.Set(order.GetId(), order)
	log.Debug().Str("order", order.GetId()).Str("symbol", order.Symbol).
		Str("side", order.Side.String()).Msg("Order created")

	return order, nil
}

func (c *Core) GetOrder(id string) (*api.Order, error) {
	o, ok := c.orders.Get(id)
	if !ok {
		return nil, ErrNoData
	}
	order, ok := o.(*api.Order)
	if !ok {
		return nil, ErrNoData
	}

	return order, nil
}

func (c *Core) CancelOrder(id string) (*api.Order, error) {
	o, ok := c.orders.Get(id)
	if !ok {
		return nil, ErrNoData
	}
	order, ok2 := o.(*api.Order)
	if !ok2 {
		return nil, ErrNoData
	}
	c.orders.Del(id)
	order.Status = api.OrderStatus_CANCELED

	if err := c.updateBalance(order); err != nil {
		return nil, err
	}
	log.Debug().Str("order", order.Id).Str("user", order.UserId).Str("symbol", order.Symbol).
		Str("side", order.Side.String()).Msg("Order canceled")

	return order, nil
}

func (c *Core) updateState(state *ExchangeState) (updated bool) {
	for kv := range c.orders.Iter() {
		order, ok := kv.Value.(*api.Order)
		if !ok {
			continue
		}
		price, err := decimal.NewFromString(order.Price)
		if err != nil {
			log.Error().Err(err).Str("order", order.Id).Str("price", order.Price).Msg("can't parse")
		}
		if price.LessThan(state.Low) || price.GreaterThan(state.High) {
			continue
		}
		total, err := decimal.NewFromString(order.Total)
		if err != nil {
			log.Error().Err(err).Str("order", order.Id).Str("total", order.Total).Msg("can't parse")
		}
		if order.Side == api.OrderSide_BUY && total.GreaterThan(state.AssetVolume) ||
			order.Side == api.OrderSide_SELL && total.GreaterThan(state.BaseVolume) {
			log.Panic().Str("side", order.Side.String()).Str("total", order.Total).
				Str("asset", state.AssetVolume.String()).Str("base", state.BaseVolume.String()).
				Int64("ts", state.Unix).Msg("can't close order in one kline. Need to use PARTIAL_FILLED")
		}

		updated = true
		order.Status = api.OrderStatus_FILLED
		log.Debug().Str("order", order.Id).Str("user", order.UserId).Str("symbol", order.Symbol).
			Str("side", order.Side.String()).Msg("Order filled")
		if err := c.updateBalance(order); err != nil {
			log.Panic().Err(err).Str("order", order.Id).Msg("can't update balance")

			continue
		}
		if c.callback != nil {
			c.callback(order)
		}
		c.orders.Del(order.Id)
	}

	return updated
}

func (c *Core) updateBalance(o *api.Order) error {
	bb, ok := c.balances.Get(o.UserId)
	if !ok {
		return ErrEmptyBalance
	}

	balances, ok2 := bb.(*api.Balances)
	if !ok2 {
		return ErrEmptyBalance
	}
	var base, quote string
	if o.GetSide() == api.OrderSide_BUY {
		base, quote = o.Symbol[3:], o.Symbol[:3]
	} else {
		base, quote = o.Symbol[:3], o.Symbol[3:]
	}
	var input, output *api.Balance
	for _, b := range balances.GetData() {
		switch b.Asset {
		case base:
			input = b
		case quote:
			output = b
		}
	}
	if input == nil {
		input = &api.Balance{
			Asset: base,
		}
		balances.Data = append(balances.Data, input)
		c.balances.Set(o.UserId, balances)
	}
	if output == nil {
		output = &api.Balance{
			Asset: quote,
		}
		balances.Data = append(balances.Data, output)
		c.balances.Set(o.UserId, balances)
	}
	total, err := decimal.NewFromString(o.Total)
	if err != nil {
		return err
	}
	inputFree, err := decimal.NewFromString(input.GetFree())
	if err != nil {
		return err
	}
	inputLocked, err := decimal.NewFromString(input.GetLocked())
	if err != nil {
		return err
	}
	outputFree, err := decimal.NewFromString(output.Free)
	if err != nil {
		return err
	}

	//nolint:exhaustive
	switch o.GetStatus() {
	case api.OrderStatus_NEW:
		inputFree = inputFree.Sub(total)
		if inputFree.IsNegative() {
			return ErrNegativeBalance
		}
		input.Free = inputFree.String()
		input.Locked = inputLocked.Add(total).String()

	case api.OrderStatus_CANCELED:
		input.Locked = inputLocked.Sub(total).String()
		input.Free = inputFree.Add(total).String()

	case api.OrderStatus_FILLED:
		inputLocked = inputLocked.Sub(total)
		if inputLocked.IsNegative() {
			return ErrNegativeBalance
		}
		input.Locked = inputLocked.String()

		if o.GetSide() == api.OrderSide_BUY {
			qty, err := decimal.NewFromString(o.Quantity)
			if err != nil {
				return err
			}
			output.Free = outputFree.Add(qty.Sub(qty.Mul(c.commission))).String()
		} else {
			price, err := decimal.NewFromString(o.Price)
			if err != nil {
				return err
			}
			output.Free = outputFree.Add(price.Sub(price.Mul(c.commission))).String()
		}
	}
	log.Info().Str("user", o.UserId).
		Str("base_asset", input.Asset).Float64("base_free", inputFree.InexactFloat64()).Float64("base_locked", inputLocked.InexactFloat64()).
		Str("quote_asset", output.Asset).Float64("quote_free", outputFree.InexactFloat64()).Msg("Balance updated")

	return nil
}

func (c *Core) Exchange() *Exchange {
	return c.exchange
}

func (c *Core) Close() error {
	return c.exchange.Close()
}

func (c *Core) SetExchange(ctx context.Context, exchange *Exchange, file string) error {
	err := exchange.Init(ctx, file, c.updateState)
	if err != nil {
		return err
	}
	c.exchange = exchange

	return nil
}
