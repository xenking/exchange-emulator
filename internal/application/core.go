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
	ErrNoData             = errors.New("no data")
	ErrUnknownUser        = errors.New("unknown user")
	ErrEmptyBalance       = errors.New("empty balance")
	ErrNegativeBalance    = errors.New("negative balance")
	ErrNegativeSubBalance = errors.New("negative sub balance")
)

type Core struct {
	balances map[string]*api.Balances
	orders   map[string]*Order
	exchange *Exchange
	callback func(order *api.Order)

	commission              decimal.Decimal
	exchangeFile            string
	orderExpiration         int64
	orderExpirePricePercent decimal.Decimal

	orderSequence uint64

	mu sync.Mutex
}

type Order struct {
	*api.Order

	price    decimal.Decimal
	total    decimal.Decimal
	quantity decimal.Decimal
}

func NewCore(exchangeFile string, commission decimal.Decimal, cancelDuration time.Duration, cancelPricePercent int64) *Core {
	return &Core{
		balances: make(map[string]*api.Balances),
		orders:   make(map[string]*Order),

		exchangeFile:            exchangeFile,
		commission:              commission.Div(decimal.NewFromInt(100)),
		orderExpiration:         int64(cancelDuration / time.Millisecond),
		orderExpirePricePercent: decimal.NewFromInt(1 + cancelPricePercent/100),
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
	if state.Symbol == "" {
		return decimal.Decimal{}, ErrNoData
	}

	log.Debug().Str("symbol", symbol).Float64("price", state.Close.InexactFloat64()).
		Msg("Get price")

	return state.Close, nil
}

func (c *Core) CurrState() ExchangeState {
	var state ExchangeState

	wait := make(chan struct{})
	c.exchange.transactions <- func(s ExchangeState) bool {
		state = s
		close(wait)

		return true
	}
	<-wait

	return state
}

func (c *Core) GetBalance(user string) (*api.Balances, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	balances, ok := c.balances[user]
	if !ok {
		return nil, ErrUnknownUser
	}

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

	c.mu.Lock()
	defer c.mu.Unlock()

	c.balances[user] = balances

	log.Debug().Str("user", user).Msg("Balance set")
}

func (c *Core) CreateOrder(user string, order *api.Order) (o *Order, err error) {
	order.OrderId = atomic.AddUint64(&c.orderSequence, 1)
	order.Symbol = strings.ToUpper(order.Symbol)
	order.TransactTime = c.exchange.ShiftTime()
	order.Status = api.OrderStatus_NEW
	order.UserId = user

	o.price, err = decimal.NewFromString(order.GetPrice())
	if err != nil {
		return nil, err
	}
	o.quantity, err = decimal.NewFromString(order.GetQuantity())
	if err != nil {
		return nil, err
	}

	o.total = o.price.Mul(o.quantity)
	o.Order = order

	c.mu.Lock()
	defer c.mu.Unlock()

	err = c.updateBalance(o)
	if err != nil {
		return nil, err
	}

	c.orders[order.GetId()] = o

	log.Debug().Str("order", order.GetId()).Str("symbol", order.Symbol).
		Str("side", order.Side.String()).Msg("Order created")

	return o, nil
}

func (c *Core) GetOrder(id string) (*Order, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	order, ok := c.orders[id]
	if !ok {
		return nil, ErrNoData
	}

	return order, nil
}

func (c *Core) CancelOrder(id string) (*Order, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	order, ok := c.orders[id]
	if !ok {
		return nil, ErrNoData
	}
	delete(c.orders, id)

	order.Status = api.OrderStatus_CANCELED

	if err := c.updateBalance(order); err != nil {
		return nil, err
	}
	log.Debug().Str("order", order.Id).Str("user", order.UserId).Str("symbol", order.Symbol).
		Str("side", order.Side.String()).Msg("Order canceled")

	return order, nil
}

func (c *Core) updateState(state ExchangeState) (updated bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	sides := 0

	for _, order := range c.orders {

		if sides == 0 && order.Side == api.OrderSide_BUY {
			sides++
		} else if sides == 1 && order.Side == api.OrderSide_SELL {
			sides++
		}

		if order.price.LessThan(state.Low) || order.price.GreaterThan(state.High) {
			continue
		}

		if order.Side == api.OrderSide_BUY && order.total.GreaterThan(state.AssetVolume) ||
			order.Side == api.OrderSide_SELL && order.total.GreaterThan(state.BaseVolume) {
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
		}

		if c.callback != nil {
			c.callback(order.Order)
		}

		delete(c.orders, order.Id)
	}

	return updated
}

func (c *Core) updateBalance(o *Order) error {
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

	inputFree, err := decimal.NewFromString(input.GetFree())
	if err != nil {
		return err
	}
	inputLocked, err := decimal.NewFromString(input.GetLocked())
	if err != nil {
		return err
	}
	outputFree, err := decimal.NewFromString(output.GetFree())
	if err != nil {
		return err
	}

	//nolint:exhaustive
	switch o.GetStatus() {
	case api.OrderStatus_NEW:
		if o.GetSide() == api.OrderSide_BUY {
			inputFree = inputFree.Sub(o.total)
			if inputFree.IsNegative() {
				log.Error().Str("input.free", input.Free).Str("total", o.Total).Msg("new order")

				return ErrNegativeBalance
			}
			inputLocked = inputLocked.Add(o.total)
		} else {
			inputFree = inputFree.Sub(o.quantity)
			if inputFree.IsNegative() {
				log.Error().Str("input.free", input.Free).Str("qty", o.Quantity).Msg("new order")

				return ErrNegativeBalance
			}
			inputLocked = inputLocked.Add(o.quantity)
		}
		input.Free = inputFree.String()
		input.Locked = inputLocked.String()

	case api.OrderStatus_CANCELED:
		if o.GetSide() == api.OrderSide_BUY {
			input.Locked = inputLocked.Sub(o.total).String()
			input.Free = inputFree.Add(o.total).String()
		} else {
			input.Locked = inputLocked.Sub(o.quantity).String()
			input.Free = inputFree.Add(o.quantity).String()
		}

	case api.OrderStatus_FILLED:
		if o.GetSide() == api.OrderSide_BUY {
			inputLocked = inputLocked.Sub(o.total)
			outputFree = outputFree.Add(o.quantity.Sub(o.quantity.Mul(c.commission)))
		} else {
			inputLocked = inputLocked.Sub(o.quantity)
			outputFree = outputFree.Add(o.total.Sub(o.total.Mul(c.commission)))
		}
		if inputLocked.IsNegative() {
			log.Error().Str("input.locked", input.Locked).Str("total", o.Total).Msg("order filled")

			return ErrNegativeSubBalance
		}
		input.Locked = inputLocked.String()
		output.Free = outputFree.String()
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

func (c *Core) SetExchange(ctx context.Context, exchange *Exchange, file string, delay time.Duration, offset int64) error {
	err := exchange.Init(ctx, file, c.updateState, delay, offset)
	if err != nil {
		return err
	}
	c.exchange = exchange

	return nil
}
