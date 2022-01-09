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

		d := csv.NewDecoder(r).Header(false)
		var row string
		var err error
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
	exchangeInfoErr  error
	exchangeInfoOnce sync.Once
)

func LoadExchangeInfo(filename string) ([]byte, error) {
	exchangeInfoOnce.Do(func() {
		f, err := os.Open(filename)
		if err != nil {
			exchangeInfoErr = err

			return
		}
		defer f.Close()
		exchangeInfo, _ = io.ReadAll(f)
	})

	return exchangeInfo, exchangeInfoErr
}
