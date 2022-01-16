package application

import (
	"context"
	"os"
	"sync"
	"sync/atomic"

	"github.com/phuslu/log"
	"github.com/segmentio/encoding/json"

	"github.com/xenking/exchange-emulator/pkg/rfile"
)

type Exchange struct {
	shutdown     context.CancelFunc
	transactions chan transaction
	lock         chan struct{}
	offset       int64
	run          uint32
}

type transaction func(*ExchangeState) bool

func NewExchange(ctx context.Context, file string, cb transaction) (*Exchange, error) {
	e := &Exchange{
		transactions: make(chan transaction),
		lock:         make(chan struct{}),
	}
	err := e.initData(ctx, file, cb)

	return e, err
}

func (e *Exchange) initData(ctx context.Context, file string, cb transaction) error {
	ctx, e.shutdown = context.WithCancel(ctx)
	rf, err := rfile.Open(file)
	if err != nil {
		return err
	}
	data, errch := ParseCSV(ctx, rf)

	go e.dataLoop(ctx, data, cb)
	go e.errorLoop(ctx, errch)

	return nil
}

func (e *Exchange) dataLoop(ctx context.Context, data <-chan *ExchangeState, cb transaction) {
	var off int64
	var opened bool
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
				states = data
				off = atomic.LoadInt64(&e.offset)
			case states != nil && running == 0:
				states = nil
			}
		case t := <-e.transactions:
			t(state)
		case state, opened = <-states:
			if !opened {
				e.Stop()

				return
			}
			if state.Unix < off {
				continue
			}
			if updated := cb(state); updated {
				e.Stop()
			}
		}
	}
}

func (e *Exchange) errorLoop(ctx context.Context, errch chan error) {
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-errch:
			if !ok {
				return
			}
			log.Error().Err(err).Msg("parse error")
		}
	}
}

func (e *Exchange) Stop() {
	if ok := atomic.CompareAndSwapUint32(&e.run, 1, 0); ok {
		log.Debug().Msg("exchange stopped")
		e.lock <- struct{}{}
	}
}

func (e *Exchange) Start() {
	if ok := atomic.CompareAndSwapUint32(&e.run, 0, 1); ok {
		log.Debug().Msg("exchange started")
		e.lock <- struct{}{}
	}
}

func (e *Exchange) SetOffset(offset int64) {
	log.Debug().Int64("offset", offset).Msg("set offset")
	atomic.StoreInt64(&e.offset, offset)
}

func (e *Exchange) Close() error {
	e.shutdown()
	close(e.lock)
	close(e.transactions)

	return nil
}

var (
	exchangeInfo     map[string]interface{}
	exchangeInfoErr  error
	exchangeInfoOnce sync.Once
)

func LoadExchangeInfo(filename string) (map[string]interface{}, error) {
	exchangeInfoOnce.Do(func() {
		f, err := os.Open(filename)
		if err != nil {
			exchangeInfoErr = err

			return
		}
		defer f.Close()
		exchangeInfoErr = json.NewDecoder(f).Decode(exchangeInfo)
	})

	return exchangeInfo, exchangeInfoErr
}
