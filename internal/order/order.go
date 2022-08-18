package order

import (
	"context"
	"github.com/go-faster/errors"
	"strings"

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
	iterations   chan func(data []*Order)
	iterDone     chan struct{}
	data         []*Order
}

func New() *Tracker {
	return &Tracker{
		transactions: make(chan transaction),
		iterations:   make(chan func(data []*Order)),
		iterDone:     make(chan struct{}),
	}
}

var (
	ErrNotFound = errors.New("order not found")
)

type TransactionType int8

const (
	TypeAdd TransactionType = iota + 1
	TypeGet
	TypeCancel
	TypeUpdate
	TypeRange
)

type transaction struct {
	action   func(data *Order) bool
	iterator func(data []*Order)
	ID       string
	Type     TransactionType
}

func (t *Tracker) Start(ctx context.Context) {
	orderSequence := uint64(0)
	data := make(map[string]*Order)

	go t.iterator(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case tt := <-t.transactions:
			switch tt.Type {
			case TypeAdd:
				orderSequence++
				order := &Order{
					internalOrderID: orderSequence,
				}

				if !tt.action(order) {
					continue
				}

				data[order.Id] = order
				t.data = append(t.data, order)
			case TypeGet:
				order, _ := data[tt.ID]
				tt.action(order)
			case TypeCancel:
				delete(data, tt.ID)
				for i, o := range t.data {
					if o.Id == tt.ID {
						t.data = append(t.data[:i], t.data[i+1:]...)
						break
					}
				}
				tt.action(nil)
			case TypeUpdate:
				order, _ := data[tt.ID]
				tt.action(order)
			case TypeRange:
				select {
				case <-ctx.Done():
					return
				case t.iterations <- tt.iterator:
					select {
					case <-ctx.Done():
						return
					case <-t.iterDone:
					}
				}
			}
		}
	}
}

func (t *Tracker) iterator(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case iterator := <-t.iterations:
			iterator(t.data)

			select {
			case <-ctx.Done():
				return
			case t.iterDone <- struct{}{}:
			}
		}
	}
}

func (t *Tracker) Add(order *api.Order, timestamp int64) *Order {
	var newOrder *Order

	errc := make(chan error)
	t.transactions <- transaction{
		Type: TypeAdd,
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
		Type: TypeGet,
		ID:   id,
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
		Type: TypeCancel,
		ID:   id,
		action: func(o *Order) bool {
			order = o
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
		Type: TypeUpdate,
		ID:   id,
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
		Type: TypeRange,
		iterator: func(data []*Order) {
			cb(data)
			close(done)
		},
	}
	<-done
}
