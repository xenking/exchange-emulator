package balance

import (
	"context"

	"github.com/go-faster/errors"

	"github.com/xenking/decimal"
)

type Asset struct {
	Free   decimal.Decimal
	Locked decimal.Decimal
	Name   string
}

type Tracker struct {
	transactions chan transaction
	iterations   chan iterator
}

func New() *Tracker {
	return &Tracker{
		transactions: make(chan transaction),
		iterations:   make(chan iterator),
	}
}

type transaction struct {
	asset  string
	action func(data *Asset)
}

type iterator func(data map[string]*Asset)

func (t *Tracker) Start(ctx context.Context) {
	data := make(map[string]*Asset)

	for {
		select {
		case <-ctx.Done():
			return
		case it := <-t.iterations:
			it(data)
		case tt := <-t.transactions:
			item, ok := data[tt.asset]
			if !ok {
				item = &Asset{
					Name: tt.asset,
				}
				data[tt.asset] = item
			}

			tt.action(item)
		}
	}
}

var (
	ErrNegative = errors.New("balance is negative")
)

func (t *Tracker) NewTransaction(asset string, f func(data *Asset) error) error {
	errc := make(chan error)
	t.transactions <- transaction{
		asset: asset,
		action: func(data *Asset) {
			errc <- f(data)
			close(errc)
		},
	}
	return <-errc
}

func (t *Tracker) List() []Asset {
	done := make(chan struct{})
	var resp []Asset
	t.iterations <- func(data map[string]*Asset) {
		for _, asset := range data {
			resp = append(resp, *asset)
		}
		close(done)
	}
	<-done
	return resp
}

func (t *Tracker) Set(balances []Asset) {
	t.iterations <- func(data map[string]*Asset) {
		for _, balance := range balances {
			data[balance.Name] = &balance
		}
	}
}
