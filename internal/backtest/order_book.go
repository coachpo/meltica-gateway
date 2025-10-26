package backtest

import (
	"sort"

	"github.com/coachpo/meltica/internal/schema"
	"github.com/shopspring/decimal"
)

// OrderBook represents a simplified order book for a single instrument.
type OrderBook struct {
	bids []*schema.OrderRequest
	asks []*schema.OrderRequest
}

// NewOrderBook creates a new order book.
func NewOrderBook() *OrderBook {
	return &OrderBook{
		bids: make([]*schema.OrderRequest, 0),
		asks: make([]*schema.OrderRequest, 0),
	}
}

// AddOrder adds an order to the order book.
func (ob *OrderBook) AddOrder(order *schema.OrderRequest) {
	switch order.Side {
	case schema.TradeSideBuy:
		ob.bids = append(ob.bids, order)
		sort.Slice(ob.bids, func(i, j int) bool {
			p1, _ := decimal.NewFromString(*ob.bids[i].Price)
			p2, _ := decimal.NewFromString(*ob.bids[j].Price)
			return p1.GreaterThan(p2)
		})
	case schema.TradeSideSell:
		ob.asks = append(ob.asks, order)
		sort.Slice(ob.asks, func(i, j int) bool {
			p1, _ := decimal.NewFromString(*ob.asks[i].Price)
			p2, _ := decimal.NewFromString(*ob.asks[j].Price)
			return p1.LessThan(p2)
		})
	}
}

// Match attempts to match a new order against the existing orders in the book.
func (ob *OrderBook) Match(order *schema.OrderRequest) []*schema.TradePayload {
	var trades []*schema.TradePayload

	switch order.Side {
	case schema.TradeSideBuy:
		for i := len(ob.asks) - 1; i >= 0; i-- {
			ask := ob.asks[i]
			buyPrice, _ := decimal.NewFromString(*order.Price)
			askPrice, _ := decimal.NewFromString(*ask.Price)

			if buyPrice.GreaterThanOrEqual(askPrice) {
				// Match found.
				trade := &schema.TradePayload{
					Price:    *ask.Price,
					Quantity: order.Quantity, // This is a simplified matching logic.
				}
				trades = append(trades, trade)

				// Remove the matched order from the book.
				ob.asks = append(ob.asks[:i], ob.asks[i+1:]...)
				// For simplicity, we assume the new order is fully filled.
				return trades
			}
		}
	case schema.TradeSideSell:
		for i := len(ob.bids) - 1; i >= 0; i-- {
			bid := ob.bids[i]
			sellPrice, _ := decimal.NewFromString(*order.Price)
			bidPrice, _ := decimal.NewFromString(*bid.Price)

			if sellPrice.LessThanOrEqual(bidPrice) {
				// Match found.
				trade := &schema.TradePayload{
					Price:    *bid.Price,
					Quantity: order.Quantity, // This is a simplified matching logic.
				}
				trades = append(trades, trade)

				// Remove the matched order from the book.
				ob.bids = append(ob.bids[:i], ob.bids[i+1:]...)
				// For simplicity, we assume the new order is fully filled.
				return trades
			}
		}
	}

	// If no match is found, add the order to the book.
	ob.AddOrder(order)
	return trades
}
