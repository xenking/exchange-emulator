package parser

import (
	"encoding/binary"

	"github.com/xenking/decimal"

	"github.com/xenking/exchange-emulator/pkg/utils"
)

// header
// unix,date,symbol,open,high,low,close,Volume ETH,Volume USDT,tradecount

type ExchangeState struct {
	Open  decimal.Decimal `json:"open"`
	High  decimal.Decimal `json:"high"`
	Low   decimal.Decimal `json:"low"`
	Close decimal.Decimal `json:"close"`
	Raw   []byte          `json:"-"`
	Unix  int64           `json:"unix"`
}

func (e ExchangeState) MarshalJSON() ([]byte, error) {
	b := make([]byte, 0, 90)
	b = e.AppendMarshalJSON(b)
	return b, nil
}

func (e ExchangeState) AppendMarshalJSON(b []byte) []byte {
	// {"open":"3690.57","high":"3691.03","low":"3688.00","close":"3690.09","unix":1640995440000}
	b = append(b, `{"open":"`...)
	b = utils.AppendDecimal(b, e.Open)
	b = appendZeroExponent(b, e.Open.Exponent())
	b = append(b, `","high":"`...)
	b = utils.AppendDecimal(b, e.High)
	b = appendZeroExponent(b, e.High.Exponent())
	b = append(b, `","low":"`...)
	b = utils.AppendDecimal(b, e.Low)
	b = appendZeroExponent(b, e.Low.Exponent())
	b = append(b, `","close":"`...)
	b = utils.AppendDecimal(b, e.Close)
	b = appendZeroExponent(b, e.Close.Exponent())
	b = append(b, `","unix":`...)
	b = binary.BigEndian.AppendUint64(b, uint64(e.Unix))
	b = append(b, '}')
	return b
}

func (e ExchangeState) AppendEncoded(b []byte) []byte {
	// 3690.09|1640995440000 == 15 raw bytes
	b = binary.BigEndian.AppendUint64(b, uint64(e.Unix))
	b = utils.AppendDecimal(b, e.Close)
	b = appendZeroExponent(b, e.Close.Exponent())
	return b
}

func appendZeroExponent(dst []byte, exp int32) []byte {
	if exp != 0 {
		return dst
	}
	return append(dst, '.', '0', '0')
}

type exchangeState struct {
	Unix        []byte `csv:"unix"`
	Date        []byte `csv:"date"`
	Symbol      []byte `csv:"symbol"`
	Open        []byte `csv:"open"`
	High        []byte `csv:"high"`
	Low         []byte `csv:"low"`
	Close       []byte `csv:"close"`
	BaseVolume  []byte `csv:"Volume ETH"`
	AssetVolume []byte `csv:"Volume USDT"`
	Trades      []byte `csv:"trades"`
}

func (s *exchangeState) Parse() ExchangeState {
	e := ExchangeState{}
	e.Unix, _ = utils.ParseUintBytes(s.Unix)
	e.Open, _ = utils.ParseDecimalBytes(s.Open[:len(s.Open)-6])
	e.High, _ = utils.ParseDecimalBytes(s.High[:len(s.High)-6])
	e.Low, _ = utils.ParseDecimalBytes(s.Low[:len(s.Low)-6])
	e.Close, _ = utils.ParseDecimalBytes(s.Close[:len(s.Close)-6])
	return e
}
