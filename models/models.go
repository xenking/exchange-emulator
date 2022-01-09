package models

import (
	"time"

	"github.com/xenking/decimal"

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

type Op uint8

const (
	OpExchangeStart Op = iota + 1
	OpExchangeStop
	OpExchangeOffset

	OpExchangeInfo
	OpPrice

	OpBalanceGet
	OpBalanceSet

	OpOrderCreate
	OpOrderGet
	OpOrderCancel

	OpOrderUpdate
)

type PriceReq struct {
	Operation
	Symbol string          `json:"symbol"`
	Price  decimal.Decimal `json:"price"`
}

type BalancesReq struct {
	Operation
	Balances []*Balance `json:"balances"`
}

type Balance struct {
	Asset  string          `json:"asset"`
	Free   decimal.Decimal `json:"free"`
	Locked decimal.Decimal `json:"locked"`
}

type Order struct {
	Operation
	Symbol   string          `json:"symbol"`
	ID       string          `json:"clientOrderId"`
	Type     string          `json:"type"`
	Side     string          `json:"side"`
	Status   OrderStatus     `json:"status"`
	Price    decimal.Decimal `json:"price"`
	Quantity decimal.Decimal `json:"origQty"`
	Total    decimal.Decimal `json:"total"`
	OrderID  uint64          `json:"orderId"`
	UserID   uint64          `json:"userId,omitempty"`
}

type OrderStatus string

const (
	OrderStatusNew      OrderStatus = "NEW"
	OrderStatusFilled   OrderStatus = "FILLED"
	OrderStatusCanceled OrderStatus = "CANCELED"
)

const (
	OrderSideBuy  = "BUY"
	OrderSideSell = "SELL"
)

type Operation struct {
	Op Op `json:"operation,omitempty"`
}

type OffsetReq struct {
	Operation
	Offset int64 `json:"offset"`
}

type OrderIDReq struct {
	Operation
	ID string `json:"clientOrderId"`
}
