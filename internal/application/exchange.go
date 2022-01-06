package application

import (
	"context"
	"os"
	"sync/atomic"

	"github.com/phuslu/log"
	"github.com/segmentio/encoding/json"
)

type Exchange struct {
	Transactions chan Transaction
	lock         chan struct{}
	run          uint32
	offset       int64
}

type Transaction func(*ExchangeState) bool

func NewExchange(ctx context.Context, file string, updateCallback Transaction) (*Exchange, error) {
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
		Transactions: make(chan Transaction),
		lock:         make(chan struct{}),
	}

	go e.watcherLoop(ctx, updates, updateCallback)

	return e, nil
}

func (e *Exchange) watcherLoop(ctx context.Context, updates <-chan *ExchangeState, cb Transaction) {
	var offset int64
	var state *ExchangeState
	var states <-chan *ExchangeState
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.lock:
			running := atomic.LoadUint32(&e.run)
			switch {
			case states == nil && running == 1:
				states = updates
				offset = atomic.LoadInt64(&e.offset)
			case states != nil && running == 0:
				states = nil
			}
		case t := <-e.Transactions:
			t(state)
		case state = <-states:
			if state.Unix < offset {
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

type Offset struct {
	Offset int64 `json:"offset"`
}

func (e *Exchange) Offset(data []byte) error {
	o := &Offset{}

	if err := json.Unmarshal(data, o); err != nil {
		return err
	}
	atomic.StoreInt64(&e.offset, o.Offset)

	return nil
}
