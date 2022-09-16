package parser

import (
	"context"
	"time"
)

type Listener struct {
	states chan ExchangeState
	data   []ExchangeState
	delay  time.Duration
}

func (p *Parser) NewListener() *Listener {
	return &Listener{
		states: make(chan ExchangeState),
		data:   p.data,
		delay:  p.delay,
	}
}

func (l *Listener) Start(ctx context.Context) {
	defer close(l.states)

	idx := 0
	last := len(l.data) - 1

	select {
	case <-ctx.Done():
		return
	case l.states <- l.data[idx]:
		idx++
	}

	ticker := time.NewTicker(l.delay)
	defer ticker.Stop()
	for range ticker.C {
		// check for EOF condition
		if idx == last {
			return
		}

		select {
		case <-ctx.Done():
			return
		case l.states <- l.data[idx]:
			idx++
		}
	}
}

func (l *Listener) ExchangeStates() <-chan ExchangeState {
	return l.states
}

// TODO: use this decimal library https://github.com/db47h/decimal/tree/math
// OR THIS https://github.com/ericlagergren/decimal (need to fork)
