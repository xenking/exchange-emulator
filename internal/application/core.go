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
)

type Core struct {
	*Exchange
	CompleteHandler func(order *Order)
	UserBalances    *hashmap.HashMap // map[userID][]*Balance
	Orders          *hashmap.HashMap // map[orderID]*Order

	prices        *hashmap.HashMap // map[price]*Order
	commission    decimal.Decimal
	exchangeFile  string
	orderSequence uint64
}

func NewCore(exchangeFile string, commission decimal.Decimal) *Core {
	return &Core{
		UserBalances: &hashmap.HashMap{},
		Orders:       &hashmap.HashMap{},

		prices:       &hashmap.HashMap{},
		exchangeFile: exchangeFile,
		commission:   commission,
	}
}

func (c *Core) SetSymbolData(ctx context.Context, symbol, file string) (err error) {
	c.Exchange, err = NewExchange(ctx, symbol, file, c.watch)

	return
}

func (c *Core) CurrState(symbol string) *ExchangeState {
	var state *ExchangeState
	wait := make(chan struct{})
	c.Transactions <- func(s *ExchangeState) bool {
		state = s
		close(wait)

		return true
	}
	<-wait

	return state
}

func (c *Core) ExchangeInfo(w io.Writer) {
	_, _ = w.Write(LoadExchangeInfo(c.exchangeFile))
}

type SymbolPrice struct {
	Symbol string          `json:"symbol"`
	Price  decimal.Decimal `json:"price"`
}

var (
	ErrEmptySymbol  = errors.New("empty symbol")
	ErrNoData       = errors.New("no data")
	ErrUnknownUser  = errors.New("unknown user")
	ErrEmptyBalance = errors.New("empty balance")
)

func (c *Core) GetPrice(w io.Writer, data []byte) error {
	r := &SymbolPrice{}
	if err := json.Unmarshal(data, r); err != nil {
		return err
	}
	if r.Symbol == "" {
		return ErrEmptySymbol
	}
	state := c.CurrState(r.Symbol)
	if state == nil {
		return ErrNoData
	}
	r.Price = state.Close
	err := json.NewEncoder(w).Encode(r)

	return err
}

type Balance struct {
	Asset  string          `json:"asset"`
	Free   decimal.Decimal `json:"free"`
	Locked decimal.Decimal `json:"locked"`
}

func (c *Core) SetBalance(w io.Writer, user uint64, data []byte) error {
	var bb []*Balance
	if err := json.Unmarshal(data, &bb); err != nil {
		return err
	}
	c.UserBalances.Set(user, bb)
	_, _ = w.Write(data)

	return nil
}

func (c *Core) GetBalance(w io.Writer, user uint64) error {
	b, ok := c.UserBalances.Get(user)
	if !ok {
		return ErrUnknownUser
	}
	err := json.NewEncoder(w).Encode(b)

	return err
}

type Order struct {
	Symbol   string          `json:"symbol"`
	ID       string          `json:"clientOrderId"`
	Type     string          `json:"type"`
	Side     string          `json:"side"`
	Status   OrderStatus     `json:"status"`
	Price    decimal.Decimal `json:"price"`
	Quantity decimal.Decimal `json:"origQty"`
	Total    decimal.Decimal `json:"total"`
	OrderID  uint64          `json:"orderId"`
	UserID   uint64          `json:"userId"`
}

type OrderStatus string

const (
	OrderStatusNew      OrderStatus = "NEW"
	OrderStatusFilled   OrderStatus = "FILLED"
	OrderStatusCanceled OrderStatus = "CANCELED"
)

const (
	OrderSideBuy  = "BUY"
	OrderSideSell = "SELL"
)

func (c *Core) CreateOrder(w io.Writer, user uint64, data []byte) error {
	o := &Order{}
	if err := json.Unmarshal(data, o); err != nil {
		return err
	}
	o.UserID = user
	o.OrderID = atomic.AddUint64(&c.orderSequence, 1)
	o.Status = OrderStatusNew
	if o.Side == OrderSideBuy {
		o.Total = o.Price.Mul(o.Quantity)
	} else {
		o.Total = o.Quantity.Div(o.Price)
	}
	c.Orders.Set(o.ID, o)
	c.prices.Set(o.Price, o)

	if err := c.updateBalance(o); err != nil {
		return err
	}
	err := json.NewEncoder(w).Encode(o)

	return err
}

type OrderID struct {
	ID string `json:"clientOrderId"`
}

func (c *Core) GetOrder(w io.Writer, data []byte) error {
	o := &OrderID{}
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
	req := &OrderID{}
	if err := json.Unmarshal(data, req); err != nil {
		return err
	}
	o, ok := c.Orders.Get(req.ID)
	if !ok {
		return ErrNoData
	}
	order, ok2 := o.(*Order)
	if !ok2 {
		return ErrNoData
	}
	c.prices.Del(order.Price)
	c.Orders.Del(order.Price)
	order.Status = OrderStatusCanceled

	if err := c.updateBalance(order); err != nil {
		return err
	}

	err := json.NewEncoder(w).Encode(order)

	return err
}

func (c *Core) watch(state *ExchangeState) (updated bool) {
	for kv := range c.prices.Iter() {
		price, ok := kv.Key.(decimal.Decimal)
		if !ok {
			continue
		}
		if price.LessThan(state.Low) || price.GreaterThan(state.High) {
			continue
		}
		order, ok2 := kv.Value.(*Order)
		if !ok2 {
			continue
		}
		updated = true
		order.Status = OrderStatusFilled
		if err := c.updateBalance(order); err != nil {
			log.Error().Err(err).Str("order", order.ID).Msg("can't update balance")

			continue
		}
		c.CompleteHandler(order)
		c.Orders.Del(order.ID)
		c.prices.Del(order.Price)
	}

	return
}

var zero = decimal.NewFromInt(0)

func (c *Core) updateBalance(o *Order) error {
	bb, ok := c.UserBalances.Get(o.UserID)
	if !ok {
		return ErrEmptyBalance
	}
	balances, ok2 := bb.([]*Balance)
	if !ok2 {
		return ErrEmptyBalance
	}
	var base, quote string
	if o.Side == OrderSideBuy {
		base, quote = o.Symbol[3:], o.Symbol[:3]
	} else {
		base, quote = o.Symbol[:3], o.Symbol[3:]
	}
	var input, output *Balance
	for _, b := range balances {
		switch b.Asset {
		case base:
			input = b
		case quote:
			output = b
		}
	}
	if input == nil {
		input = &Balance{
			Asset:  base,
			Free:   zero,
			Locked: zero,
		}
	}
	if output == nil {
		output = &Balance{
			Asset:  quote,
			Free:   zero,
			Locked: zero,
		}
	}

	switch o.Status {
	case OrderStatusNew:
		input.Free = input.Free.Sub(o.Total)
		input.Locked = input.Locked.Add(o.Total)
	case OrderStatusCanceled:
		input.Locked = input.Locked.Sub(o.Total)
		input.Free = input.Free.Add(o.Total)
	case OrderStatusFilled:
		input.Locked = input.Locked.Sub(o.Total)
		output.Free = output.Free.Add(o.Quantity.Sub(o.Quantity.Mul(c.commission)))
	}

	return nil
}
