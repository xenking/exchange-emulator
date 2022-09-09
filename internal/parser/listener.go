package parser

import (
	"context"
	"time"
)

type Listener struct {
	states <-chan ExchangeState
	close  context.CancelFunc
}

func (p *Listener) ExchangeStates() <-chan ExchangeState {
	return p.states
}

func (p *Listener) Close() {
	p.close()
}

func (p *Parser) NewListener(gCtx context.Context) (*Listener, error) {
	states := make(chan ExchangeState)
	ctx, cancel := context.WithCancel(gCtx)

	go func(ctx context.Context) {
		defer close(states)

		idx := 0
		last := len(p.data) - 1

		select {
		case <-ctx.Done():
			return
		case states <- p.data[idx]:
			idx++
		}

		ticker := time.NewTicker(p.delay)
		defer ticker.Stop()
		for range ticker.C {
			// check for EOF condition
			if idx == last {
				return
			}

			select {
			case <-ctx.Done():
				return
			case states <- p.data[idx]:
				idx++
			}
		}
	}(ctx)

	return &Listener{
		states: states,
		close:  cancel,
	}, nil
}

// TODO: use this decimal library https://github.com/db47h/decimal/tree/math
// OR THIS https://github.com/ericlagergren/decimal (need to fork)
