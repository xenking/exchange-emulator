package application

import (
	"context"
	"os"
	"sync/atomic"

	"github.com/phuslu/log"
	"github.com/segmentio/encoding/json"

	"github.com/xenking/exchange-emulator/models"
)

type Exchange struct {
	Transactions chan transaction
	lock         chan struct{}
	run          uint32
	offset       int64
}

type transaction func(*models.ExchangeState) bool

func NewExchange(ctx context.Context, file string, updateCallback transaction) (*Exchange, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	updates, errc := ParseCSV(ctx, f)
	go func() {
		for err := range errc {
			log.Error().Err(err).Msg("parse error")
		}
	}()
	e := &Exchange{
		Transactions: make(chan transaction),
		lock:         make(chan struct{}),
	}

	go e.watcherLoop(ctx, updates, updateCallback)

	return e, nil
}

func (e *Exchange) watcherLoop(ctx context.Context, updates <-chan *models.ExchangeState, cb transaction) {
	var off int64
	var state *models.ExchangeState
	var states <-chan *models.ExchangeState
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.lock:
			running := atomic.LoadUint32(&e.run)
			switch {
			case states == nil && running == 1:
				states = updates
				off = atomic.LoadInt64(&e.offset)
			case states != nil && running == 0:
				states = nil
			}
		case t := <-e.Transactions:
			t(state)
		case state = <-states:
			if state.Unix < off {
				continue
			}
			if updated := cb(state); updated {
				e.Stop()
			}
		}
	}
}

func (e *Exchange) Stop() {
	if ok := atomic.CompareAndSwapUint32(&e.run, 1, 0); ok {
		e.lock <- struct{}{}
	}
}

func (e *Exchange) Start() {
	if ok := atomic.CompareAndSwapUint32(&e.run, 0, 1); ok {
		e.lock <- struct{}{}
	}
}

type offset struct {
	Offset int64 `json:"offset"`
}

func (e *Exchange) Offset(data []byte) error {
	o := &offset{}

	if err := json.Unmarshal(data, o); err != nil {
		return err
	}
	atomic.StoreInt64(&e.offset, o.Offset)

	return nil
}
