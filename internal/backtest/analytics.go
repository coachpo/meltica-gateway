// Package backtest implements historical simulation utilities for Meltica strategies.
package backtest

import (
	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/schema"
)

// Analytics captures cumulative performance statistics for a backtest run.
type Analytics struct {
	TotalOrders  int
	FilledOrders int
	TotalVolume  decimal.Decimal
	GrossPnL     decimal.Decimal
	Fees         decimal.Decimal
	NetPnL       decimal.Decimal
	MaxDrawdown  decimal.Decimal

	cash       decimal.Decimal
	positions  map[string]decimal.Decimal
	lastPrices map[string]decimal.Decimal
	peakEquity decimal.Decimal
}

func newAnalytics() *Analytics {
	return &Analytics{
		TotalOrders:  0,
		FilledOrders: 0,
		TotalVolume:  decimal.Zero,
		GrossPnL:     decimal.Zero,
		Fees:         decimal.Zero,
		NetPnL:       decimal.Zero,
		MaxDrawdown:  decimal.Zero,
		cash:         decimal.Zero,
		positions:    make(map[string]decimal.Decimal),
		lastPrices:   make(map[string]decimal.Decimal),
		peakEquity:   decimal.Zero,
	}
}

func (a *Analytics) clone() Analytics {
	snapshot := Analytics{
		TotalOrders:  a.TotalOrders,
		FilledOrders: a.FilledOrders,
		TotalVolume:  a.TotalVolume,
		GrossPnL:     a.GrossPnL,
		Fees:         a.Fees,
		NetPnL:       a.NetPnL,
		MaxDrawdown:  a.MaxDrawdown,
		cash:         a.cash,
		peakEquity:   a.peakEquity,
		positions:    make(map[string]decimal.Decimal, len(a.positions)),
		lastPrices:   make(map[string]decimal.Decimal, len(a.lastPrices)),
	}
	for key, val := range a.positions {
		snapshot.positions[key] = val
	}
	for key, val := range a.lastPrices {
		snapshot.lastPrices[key] = val
	}
	return snapshot
}

func (a *Analytics) recordOrder() {
	a.TotalOrders++
}

func (a *Analytics) updateMarketPrice(symbol string, price decimal.Decimal) {
	if price.LessThanOrEqual(decimal.Zero) {
		return
	}
	a.lastPrices[symbol] = price
	a.recomputeEquity()
}

func (a *Analytics) recordFill(symbol string, side schema.TradeSide, quantity, price, fee decimal.Decimal) {
	if quantity.LessThanOrEqual(decimal.Zero) {
		return
	}
	notional := quantity.Mul(price)
	switch side {
	case schema.TradeSideSell:
		a.cash = a.cash.Add(notional)
		a.positions[symbol] = a.positions[symbol].Sub(quantity)
	case schema.TradeSideBuy:
		a.cash = a.cash.Sub(notional)
		a.positions[symbol] = a.positions[symbol].Add(quantity)
	default:
		return
	}
	if a.positions[symbol].Equal(decimal.Zero) {
		delete(a.positions, symbol)
	}
	a.TotalVolume = a.TotalVolume.Add(quantity)
	a.FilledOrders++
	a.Fees = a.Fees.Add(fee)
	a.lastPrices[symbol] = price
	a.recomputeEquity()
}

func (a *Analytics) recomputeEquity() {
	// gross PnL is represented by cash balance
	a.GrossPnL = a.cash
	a.NetPnL = a.cash.Sub(a.Fees)
	equity := a.cash
	for symbol, position := range a.positions {
		price := a.lastPrices[symbol]
		if price.LessThanOrEqual(decimal.Zero) {
			continue
		}
		equity = equity.Add(position.Mul(price))
	}
	if equity.GreaterThan(a.peakEquity) {
		a.peakEquity = equity
	}
	drawdown := a.peakEquity.Sub(equity)
	if drawdown.GreaterThan(a.MaxDrawdown) {
		a.MaxDrawdown = drawdown
	}
}
