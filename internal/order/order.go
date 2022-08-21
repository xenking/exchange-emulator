package order

import (
	"context"
	"strings"

	"github.com/go-faster/errors"
	"github.com/phuslu/log"
	"github.com/xenking/decimal"

	"github.com/xenking/exchange-emulator/gen/proto/api"
)

type Order struct {
	*api.Order

	Price           decimal.Decimal
	Total           decimal.Decimal
	Quantity        decimal.Decimal
	internalOrderID uint64
}

type Tracker struct {
	transactions chan transaction
	signal       chan struct{}
	active       []*Order
}

func New() *Tracker {
	return &Tracker{
		transactions: make(chan transaction, 200),
		signal:       make(chan struct{}, 1),
	}
}

var (
	ErrNotFound = errors.New("order not found")
)

type transactionType int8

const (
	typeAdd transactionType = iota + 1
	typeGet
	typeCancel
	typeUpdate
	typeRange
)

type transaction struct {
	action          func(data *Order) bool
	id              string
	transactionType transactionType
}

func (t *Tracker) Start(ctx context.Context) {
	orderSequence := uint64(0)
	data := make(map[string]*Order)

	for {
		select {
		case <-ctx.Done():
			return
		case tt := <-t.transactions:
			switch tt.transactionType {
			case typeRange:
				tt.action(nil)
			case typeAdd:
				// TODO: find out why we have zero order id
				orderSequence++
				order := &Order{
					internalOrderID: orderSequence,
				}
				if !tt.action(order) {
					continue
				}

				data[order.Id] = order
				t.active = append(t.active, order)

				log.Debug().Str("id", order.Id).Str("symbol", order.Symbol).
					Int64("ts", order.TransactTime).Msg("order added")

				if len(t.active) == 1 {
					t.signal <- struct{}{}
				}
			case typeCancel:
				var order *Order
				for i, o := range t.active {
					if o.Id == tt.id {
						order = o
						t.active = append(t.active[:i], t.active[i+1:]...)
						break
					}
				}

				tt.action(order)

				if order != nil {
					log.Debug().Str("id", order.Id).Str("symbol", order.Symbol).
						Int64("ts", order.TransactTime).Msg("order deleted")
				}

				if len(t.active) == 0 {
					t.signal <- struct{}{}
				}
			case typeUpdate:
				order, ok := data[tt.id]
				tt.action(order)
				if ok {
					log.Debug().Str("id", order.Id).Str("symbol", order.Symbol).
						Int64("ts", order.TransactTime).Msg("order updated")
				}
			case typeGet:
				order, ok := data[tt.id]
				tt.action(order)

				if ok {
					log.Debug().Str("id", order.Id).Str("symbol", order.Symbol).
						Int64("ts", order.TransactTime).Msg("order get")
				}
			}
		}
	}
}

func (t *Tracker) Add(order *api.Order, timestamp int64) *Order {
	var newOrder *Order

	errc := make(chan error)
	t.transactions <- transaction{
		transactionType: typeAdd,
		action: func(o *Order) bool {
			defer close(errc)

			order.OrderId = o.internalOrderID
			order.Symbol = strings.ToUpper(order.Symbol)
			order.TransactTime = timestamp
			order.Status = api.OrderStatus_NEW

			o.Order = order

			var err error

			o.Price, err = decimal.NewFromString(order.GetPrice())
			if err != nil {
				errc <- err
				return false
			}
			o.Quantity, err = decimal.NewFromString(order.GetQuantity())
			if err != nil {
				errc <- err
				return false
			}

			o.Total = o.Price.Mul(o.Quantity)
			o.Order.Total = o.Total.String()

			newOrder = o

			return true
		},
	}
	if err := <-errc; err != nil {
		return nil
	}

	return newOrder
}

func (t *Tracker) Get(id string) *Order {
	resp := make(chan Order)
	t.transactions <- transaction{
		transactionType: typeGet,
		id:              id,
		action: func(data *Order) bool {
			if data != nil {
				resp <- *data
			}
			close(resp)
			return true
		},
	}
	order, ok := <-resp
	if !ok {
		return nil
	}

	return &order
}

func (t *Tracker) Cancel(id string) *Order {
	var order *Order
	done := make(chan struct{})
	t.transactions <- transaction{
		transactionType: typeCancel,
		id:              id,
		action: func(o *Order) bool {
			if o != nil {
				order = o
				order.Status = api.OrderStatus_CANCELED
			}

			close(done)
			return true
		},
	}
	<-done

	return order
}

func (t *Tracker) Update(id string, cb func(o *Order) bool) {
	done := make(chan struct{})
	t.transactions <- transaction{
		transactionType: typeUpdate,
		id:              id,
		action: func(o *Order) bool {
			ok := cb(o)
			close(done)

			return ok
		},
	}
	<-done
}

func (t *Tracker) Range(cb func(order []*Order)) {
	done := make(chan struct{})
	t.transactions <- transaction{
		transactionType: typeRange,
		action: func(_ *Order) bool {
			cb(t.active)
			close(done)
			return true
		},
	}
	<-done
}

func (t *Tracker) Control() <-chan struct{} {
	return t.signal
}
