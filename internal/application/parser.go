package application

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/xenking/decimal"

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

//nolint:gocritic
func ParseCSV(ctx context.Context, r io.ReadCloser) (<-chan *ExchangeState, chan error) {
	states := make(chan *ExchangeState)
	errc := make(chan error)

	go func(ctx context.Context) {
		defer close(states)
		defer close(errc)
		defer r.Close()

		d := csv.NewDecoder(r)
		row, err := d.ReadLine()
		if err != nil {
			errc <- err

			return
		}
		if _, err = d.DecodeHeader(row); err != nil {
			errc <- err

			return
		}

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

			f := &ExchangeState{}
			err = d.DecodeRecord(f, row)
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

var (
	exchangeInfo     []byte
	exchangeInfoOnce sync.Once
)

func LoadExchangeInfo(filename string) []byte {
	exchangeInfoOnce.Do(func() {
		f, fErr := os.Open(filename)
		if fErr != nil {
			return
		}
		exchangeInfo, _ = io.ReadAll(f)
		f.Close()
	})

	return exchangeInfo
}
