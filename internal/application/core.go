package application

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	m        sync.Mutex
	balances map[string]*api.Balances
	orders   map[string]*api.Order
	exchange *Exchange
	callback func(order *api.Order)

	commission    decimal.Decimal
	exchangeFile  string
	orderSequence uint64
}

func NewCore(exchangeFile string, commission decimal.Decimal) *Core {
	return &Core{
		balances: make(map[string]*api.Balances),
		orders:   make(map[string]*api.Order),

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
	c.m.Lock()
	balances, ok := c.balances[user]
	if !ok {
		c.m.Unlock()

		return nil, ErrUnknownUser
	}
	c.m.Unlock()

	return balances, nil
}

func (c *Core) SetBalance(user string, balances *api.Balances) {
	for _, b := range balances.Data {
		b.Asset = strings.ToUpper(b.Asset)
		if b.Free == "" {
			b.Free = "0"
		}
		if b.Locked == "" {
			b.Locked = "0"
		}
	}

	c.m.Lock()
	c.balances[user] = balances
	c.m.Unlock()

	log.Debug().Str("user", user).Msg("Balance set")
}

func (c *Core) CreateOrder(user string, order *api.Order) (*api.Order, error) {
	order.OrderId = atomic.AddUint64(&c.orderSequence, 1)
	order.Symbol = strings.ToUpper(order.Symbol)
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
	c.m.Lock()
	c.orders[order.GetId()] = order
	c.m.Unlock()
	log.Debug().Str("order", order.GetId()).Str("symbol", order.Symbol).
		Str("side", order.Side.String()).Msg("Order created")

	return order, nil
}

func (c *Core) GetOrder(id string) (*api.Order, error) {
	c.m.Lock()
	order, ok := c.orders[id]
	if !ok {
		c.m.Unlock()

		return nil, ErrNoData
	}
	c.m.Unlock()

	return order, nil
}

func (c *Core) CancelOrder(id string) (*api.Order, error) {
	c.m.Lock()

	order, ok := c.orders[id]
	if !ok {
		c.m.Unlock()

		return nil, ErrNoData
	}
	delete(c.orders, id)
	c.m.Unlock()

	order.Status = api.OrderStatus_CANCELED

	if err := c.updateBalance(order); err != nil {
		return nil, err
	}
	log.Debug().Str("order", order.Id).Str("user", order.UserId).Str("symbol", order.Symbol).
		Str("side", order.Side.String()).Msg("Order canceled")

	return order, nil
}

func (c *Core) updateState(state *ExchangeState) (updated bool) {
	c.m.Lock()
	defer c.m.Unlock()

	for _, order := range c.orders {
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
		c.m.Unlock()
		if err := c.updateBalance(order); err != nil {
			log.Panic().Err(err).Str("order", order.Id).Msg("can't update balance")
			c.m.Lock()

			continue
		}
		c.m.Lock()

		if c.callback != nil {
			c.callback(order)
		}
		delete(c.orders, order.Id)
	}

	return updated
}

func (c *Core) updateBalance(o *api.Order) error {
	c.m.Lock()
	defer c.m.Unlock()

	balances, ok := c.balances[o.UserId]
	if !ok {
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
			Asset:  strings.ToUpper(base),
			Free:   "0",
			Locked: "0",
		}
		balances.Data = append(balances.Data, input)
	}
	if output == nil {
		output = &api.Balance{
			Asset:  strings.ToUpper(quote),
			Free:   "0",
			Locked: "0",
		}
		balances.Data = append(balances.Data, output)
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
		Strs("base", []string{input.Asset, input.Free, input.Locked}).
		Strs("quote", []string{output.Asset, output.Free, output.Locked}).Msg("Balance updated")

	return nil
}

func (c *Core) Exchange() *Exchange {
	return c.exchange
}

func (c *Core) Close() error {
	return c.exchange.Close()
}

func (c *Core) SetExchange(ctx context.Context, exchange *Exchange, file string, delay time.Duration) error {
	err := exchange.Init(ctx, file, c.updateState, delay)
	if err != nil {
		return err
	}
	c.exchange = exchange

	return nil
}
