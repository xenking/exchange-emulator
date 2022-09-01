package balance

import (
	"context"

	"github.com/go-faster/errors"
	"github.com/phuslu/log"
	"github.com/xenking/decimal"
)

type Asset struct {
	Free   decimal.Decimal
	Locked decimal.Decimal
	Name   string
}

type Tracker struct {
	transactions chan transaction
	data         map[string]*Asset
	log          *log.Logger
}

func New() *Tracker {
	return &Tracker{
		transactions: make(chan transaction, 1024),
		data:         make(map[string]*Asset),
	}
}

type transactionType int8

const (
	typeSet transactionType = iota + 1
	typeUpdate
	typeList
)

type transaction struct {
	action          func(data *Asset)
	asset           string
	transactionType transactionType
}

func (t *Tracker) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case tt := <-t.transactions:
			switch tt.transactionType {
			case typeSet, typeList:
				tt.action(nil)
			case typeUpdate:
				item, ok := t.data[tt.asset]
				if !ok {
					item = &Asset{
						Name: tt.asset,
					}
					t.data[tt.asset] = item
				}

				tt.action(item)
			}
		}
	}
}

var ErrNegative = errors.New("balance is negative")

func (t *Tracker) NewTransaction(asset string, f func(data *Asset) error) error {
	errc := make(chan error)
	t.transactions <- transaction{
		asset:           asset,
		transactionType: typeUpdate,
		action: func(data *Asset) {
			errc <- f(data)
			close(errc)
		},
	}
	t.log.Trace().Str("asset", asset).Msg("balance transaction")
	return <-errc
}

func (t *Tracker) List() []Asset {
	done := make(chan struct{})
	var resp []Asset
	t.transactions <- transaction{
		transactionType: typeList,
		action: func(_ *Asset) {
			for _, asset := range t.data {
				resp = append(resp, *asset)
				t.log.Trace().Str("asset", asset.Name).Str("free", asset.Free.String()).
					Str("locked", asset.Locked.String()).Msg("balance get")
			}
			close(done)
		},
	}
	<-done
	return resp
}

func (t *Tracker) Set(balances []Asset) {
	t.transactions <- transaction{
		transactionType: typeSet,
		action: func(_ *Asset) {
			for _, balance := range balances {
				t.data[balance.Name] = &balance
				t.log.Trace().Str("asset", balance.Name).Str("free", balance.Free.String()).
					Str("locked", balance.Locked.String()).Msg("balance set")

			}
		},
	}
}

func (t *Tracker) SetLogger(log *log.Logger) {
	t.log = log
}
