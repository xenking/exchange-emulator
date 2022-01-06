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

	commission    decimal.Decimal
	exchangeFile  string
	orderSequence uint64
}

func NewCore(exchangeFile string, commission decimal.Decimal) *Core {
	return &Core{
		UserBalances: &hashmap.HashMap{},
		Orders:       &hashmap.HashMap{},

		exchangeFile: exchangeFile,
		commission:   commission,
	}
}

func (c *Core) SetData(ctx context.Context, file string) (err error) {
	c.Exchange, err = NewExchange(ctx, file, c.updateState)

	return
}

func (c *Core) CurrState(symbol string) *ExchangeState {
	// TODO: use symbol for multiple exchanges

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
	ErrEmptySymbol     = errors.New("empty symbol")
	ErrNoData          = errors.New("no data")
	ErrUnknownUser     = errors.New("unknown user")
	ErrEmptyBalance    = errors.New("empty balance")
	ErrNegativeBalance = errors.New("negative balance")
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
	Op       Operation       `json:"operation,omitempty"`
	Symbol   string          `json:"symbol"`
	ID       string          `json:"clientOrderId"`
	Type     string          `json:"type"`
	Side     string          `json:"side"`
	Status   OrderStatus     `json:"status"`
	Price    decimal.Decimal `json:"price"`
	Quantity decimal.Decimal `json:"origQty"`
	Total    decimal.Decimal `json:"total"`
	OrderID  uint64          `json:"orderId"`
	UserID   uint64          `json:"userId,omitempty"`
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

	if err := c.updateBalance(o); err != nil {
		return err
	}
	err := json.NewEncoder(w).Encode(o)

	return err
}

type orderID struct {
	ID string `json:"clientOrderId"`
}

func (c *Core) GetOrder(w io.Writer, data []byte) error {
	o := &orderID{}
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
	req := &orderID{}
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
	c.Orders.Del(order.Price)
	order.Status = OrderStatusCanceled

	if err := c.updateBalance(order); err != nil {
		return err
	}

	err := json.NewEncoder(w).Encode(order)

	return err
}

func (c *Core) updateState(state *ExchangeState) (updated bool) {
	for kv := range c.Orders.Iter() {
		order, ok := kv.Value.(*Order)
		if !ok {
			continue
		}
		if order.Price.LessThan(state.Low) || order.Price.GreaterThan(state.High) {
			continue
		}
		if order.Side == OrderSideBuy && order.Total.GreaterThan(state.AssetVolume) ||
			order.Side == OrderSideSell && order.Total.GreaterThan(state.BaseVolume) {
			log.Panic().Str("side", order.Side).Str("total", order.Total.String()).
				Str("asset", state.AssetVolume.String()).Str("base", state.BaseVolume.String()).
				Int64("ts", state.Unix).Msg("can't close order in one kline. Need to use PARTIAL_FILLED")
		}

		updated = true
		order.Status = OrderStatusFilled
		if err := c.updateBalance(order); err != nil {
			log.Error().Err(err).Str("order", order.ID).Msg("can't update balance")

			continue
		}
		if c.CompleteHandler != nil {
			c.CompleteHandler(order)
		}
		c.Orders.Del(order.ID)
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
		balances = append(balances, input)
		c.UserBalances.Set(o.UserID, balances)
	}
	if output == nil {
		output = &Balance{
			Asset:  quote,
			Free:   zero,
			Locked: zero,
		}
		balances = append(balances, output)
		c.UserBalances.Set(o.UserID, balances)
	}

	switch o.Status {
	case OrderStatusNew:
		input.Free = input.Free.Sub(o.Total)
		input.Locked = input.Locked.Add(o.Total)
		if input.Free.IsNegative() {
			return ErrNegativeBalance
		}
	case OrderStatusCanceled:
		input.Locked = input.Locked.Sub(o.Total)
		input.Free = input.Free.Add(o.Total)
	case OrderStatusFilled:
		input.Locked = input.Locked.Sub(o.Total)
		output.Free = output.Free.Add(o.Quantity.Sub(o.Quantity.Mul(c.commission)))
		if input.Locked.IsNegative() {
			return ErrNegativeBalance
		}
	}

	return nil
}
