package parser

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/xenking/decimal"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/pkg/csv"
	"github.com/xenking/exchange-emulator/pkg/utils"
)

type ExchangeState struct {
	Unix        int64           `csv:"unix"`
	Date        time.Time       `csv:"date"`
	Symbol      string          `csv:"symbol"`
	Open        decimal.Decimal `csv:"open"`
	High        decimal.Decimal `csv:"high"`
	Low         decimal.Decimal `csv:"low"`
	Close       decimal.Decimal `csv:"close"`
	BaseVolume  decimal.Decimal `csv:"Volume ETH"`
	AssetVolume decimal.Decimal `csv:"Volume USDT"`
	Trades      int64           `csv:"tradecount"`
}

const DateLayout = "2006-01-02 15:04:05"

func (s *ExchangeState) UnmarshalCSV(_, v []string) error {
	var err error
	s.Unix, err = utils.ParseUint(v[0])
	if err != nil {
		return err
	}
	s.Date, err = time.Parse(DateLayout, v[1])
	if err != nil {
		return err
	}
	s.Symbol = v[2]
	s.Open, err = decimal.NewFromString(v[3])
	if err != nil {
		return err
	}
	s.High, err = decimal.NewFromString(v[4])
	if err != nil {
		return err
	}
	s.Low, err = decimal.NewFromString(v[5])
	if err != nil {
		return err
	}
	s.Close, err = decimal.NewFromString(v[6])
	if err != nil {
		return err
	}
	s.BaseVolume, err = decimal.NewFromString(v[7])
	if err != nil {
		return err
	}
	s.AssetVolume, err = decimal.NewFromString(v[8])
	if err != nil {
		return err
	}
	s.Trades, err = utils.ParseUint(v[9])

	return err
}

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
	f, err := os.Open(cfg.File)
	if err != nil {
		return nil, err
	}

	data, errc := parser(ctx, f, cfg.Delay, cfg.Offset)

	return &Listener{
		data: data,
		errc: errc,
	}, nil
}

func parser(ctx context.Context, r io.ReadCloser, delay time.Duration, offset int64) (<-chan ExchangeState, chan error) {
	states := make(chan ExchangeState)
	errc := make(chan error)

	go func(ctx context.Context) {
		defer close(states)
		defer close(errc)
		defer r.Close()

		d := csv.NewDecoder(r).Header(false)
		var row string
		var err error
		first := ExchangeState{}
		for {
			row, err = d.ReadLine()
			if err != nil {
				errc <- err

				return
			}

			// check for EOF condition
			if row == "" {
				return
			}

			err = d.DecodeRecord(&first, row)
			if err != nil {
				errc <- err

				return
			}
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
			row, err = d.ReadLine()
			if err != nil {
				errc <- err

				return
			}

			// check for EOF condition
			if row == "" {
				return
			}

			f := ExchangeState{}
			err = d.DecodeRecord(&f, row)
			if err != nil {
				errc <- err

				return
			}

			select {
			case <-ctx.Done():
				return
			case states <- f:
			}
		}
	}(ctx)

	return states, errc
}
