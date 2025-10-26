package backtest

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/coachpo/meltica/internal/schema"
)

// LatencyModel returns artificial processing delays for each event.
type LatencyModel interface {
	Delay(evt *schema.Event) time.Duration
}

// ConstantLatency introduces a fixed latency for every event replayed.
type ConstantLatency struct {
	Value time.Duration
}

// Delay implements LatencyModel.
func (c ConstantLatency) Delay(_ *schema.Event) time.Duration {
	if c.Value < 0 {
		return 0
	}
	return c.Value
}

// SlippageModel adjusts execution prices to account for market impact.
type SlippageModel interface {
	Adjust(order schema.OrderRequest, book *OrderBook) decimal.Decimal
}

// BasisPointSlippage shifts the execution price by a fixed BPS amount.
type BasisPointSlippage struct {
	BPS decimal.Decimal
}

// Adjust implements SlippageModel.
func (b BasisPointSlippage) Adjust(order schema.OrderRequest, book *OrderBook) decimal.Decimal {
	if b.BPS.Equal(decimal.Zero) {
		return decimal.Zero
	}
	if order.OrderType == schema.OrderTypeMarket && book != nil {
		best := book.BestPrice(order.Side)
		if best != nil {
			price, err := decimal.NewFromString(*best)
			if err == nil {
				return price.Mul(b.BPS.Div(decimal.NewFromInt(10_000)))
			}
		}
	}
	return decimal.Zero
}

// FeeModel evaluates trading fees for executed fills.
type FeeModel interface {
	Fee(order schema.OrderRequest, fillQty, fillPrice decimal.Decimal) decimal.Decimal
}

// ProportionalFee applies maker/taker style percentage fees.
type ProportionalFee struct {
	Rate decimal.Decimal
}

// Fee implements FeeModel.
func (p ProportionalFee) Fee(_ schema.OrderRequest, fillQty, fillPrice decimal.Decimal) decimal.Decimal {
	if fillQty.LessThanOrEqual(decimal.Zero) || fillPrice.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	if p.Rate.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	return fillQty.Mul(fillPrice).Mul(p.Rate)
}
