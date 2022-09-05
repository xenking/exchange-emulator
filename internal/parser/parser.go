package parser

import (
	"context"
	"time"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/pkg/fastcsv"
)

type Listener struct {
	data <-chan ExchangeState
	errc chan error
}

func (p *Listener) ExchangeStates() <-chan ExchangeState {
	return p.data
}

func (p *Listener) Errors() <-chan error {
	return p.errc
}

func New(ctx context.Context, cfg config.ParserConfig) (*Listener, error) {
	data, errc := parser(ctx, cfg.File, cfg.Delay, cfg.Offset)

	return &Listener{
		data: data,
		errc: errc,
	}, nil
}

// TODO: use this decimal library https://github.com/db47h/decimal/tree/math
// OR THIS https://github.com/ericlagergren/decimal (need to fork)
func parser(ctx context.Context, file string, delay time.Duration, offset int64) (<-chan ExchangeState, chan error) {
	states := make(chan ExchangeState)
	errc := make(chan error)

	go func(ctx context.Context) {
		defer close(states)
		defer close(errc)

		row := &exchangeState{}
		reader, err := fastcsv.NewFileReader(file, ',', row)
		if err != nil {
			errc <- err
			return
		}

		defer reader.Close()

		first := ExchangeState{}
		for reader.Scan() {

			first = row.Parse()
			if first.Unix >= offset {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case states <- first:
		}

		ticker := time.NewTicker(delay)
		defer ticker.Stop()
		for range ticker.C {
			// check for EOF condition
			if !reader.Scan() {
				return
			}

			select {
			case <-ctx.Done():
				return
			case states <- row.Parse():
			}
		}
	}(ctx)

	return states, errc
}
