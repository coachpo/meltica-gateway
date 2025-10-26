package backtest

import (
	"testing"

	"github.com/coachpo/meltica/internal/schema"
)

func TestOrderBook_AddOrder(t *testing.T) {
	ob := NewOrderBook()

	// Add a buy order.
	buyPrice := "100"
	buyOrder := &schema.OrderRequest{
		Side:  schema.TradeSideBuy,
		Price: &buyPrice,
	}
	ob.AddOrder(buyOrder)

	if len(ob.bids) != 1 {
		t.Fatalf("expected 1 bid, got %d", len(ob.bids))
	}

	// Add a sell order.
	sellPrice := "101"
	sellOrder := &schema.OrderRequest{
		Side:  schema.TradeSideSell,
		Price: &sellPrice,
	}
	ob.AddOrder(sellOrder)

	if len(ob.asks) != 1 {
		t.Fatalf("expected 1 ask, got %d", len(ob.asks))
	}
}

func TestOrderBook_Match(t *testing.T) {
	ob := NewOrderBook()

	// Add a sell order.
	sellPrice := "101"
	sellOrder := &schema.OrderRequest{
		Side:  schema.TradeSideSell,
		Price: &sellPrice,
	}
	ob.AddOrder(sellOrder)

	// Add a buy order that matches.
	buyPrice := "101"
	buyOrder := &schema.OrderRequest{
		Side:     schema.TradeSideBuy,
		Price:    &buyPrice,
		Quantity: "1",
	}

	trades := ob.Match(buyOrder)

	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}

	if len(ob.asks) != 0 {
		t.Fatalf("expected 0 asks, got %d", len(ob.asks))
	}
}

func TestOrderBook_BestPrice(t *testing.T) {
	ob := NewOrderBook()
	price := "100"
	order := &schema.OrderRequest{
		Side:  schema.TradeSideSell,
		Price: &price,
	}
	ob.AddOrder(order)
	best := ob.BestPrice(schema.TradeSideBuy)
	if best == nil || *best != price {
		t.Fatalf("expected best ask %s, got %v", price, best)
	}
}
