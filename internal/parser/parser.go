package parser

import (
	"time"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/pkg/fastcsv"
)

type Parser struct {
	data  []ExchangeState
	delay time.Duration
}

func New(cfg config.ParserConfig) (*Parser, error) {
	p := &Parser{
		delay: cfg.ListenerDelay,
	}
	err := p.start(cfg.File, cfg.Offset)
	return p, err
}

func (p *Parser) start(file string, offset int64) error {
	row := &exchangeState{}
	reader, err := fastcsv.NewFileReader(file, ',', row)
	if err != nil {
		return err
	}

	defer reader.Close()

	var line ExchangeState
	for reader.Scan() {
		line = row.Parse()
		if line.Unix < offset {
			continue
		}

		line.Raw = make([]byte, 0, 15)
		line.Raw = line.AppendEncoded(line.Raw)
		p.data = append(p.data, line)
	}

	return nil
}
