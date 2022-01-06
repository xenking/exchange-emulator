package application

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/xenking/exchange-emulator/models"
	"github.com/xenking/exchange-emulator/pkg/csv"
)

//nolint:gocritic
func ParseCSV(ctx context.Context, r io.ReadCloser) (<-chan *models.ExchangeState, chan error) {
	states := make(chan *models.ExchangeState)
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

			f := &models.ExchangeState{}
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
