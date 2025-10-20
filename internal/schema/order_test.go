package schema

import (
	"testing"
	"time"
)

func TestOrderRequestReset(t *testing.T) {
	price := "50000.00"
	order := &OrderRequest{
		ClientOrderID: "order-123",
		ConsumerID:    "consumer-1",
		Provider:      "binance",
		Symbol:        "BTC-USD",
		Side:          TradeSideBuy,
		OrderType:     OrderTypeLimit,
		Price:         &price,
		Quantity:      "1.5",
		Timestamp:     time.Now(),
	}
	
	order.Reset()
	
	if order.ClientOrderID != "" {
		t.Errorf("ClientOrderID not reset, got %q", order.ClientOrderID)
	}
	if order.ConsumerID != "" {
		t.Errorf("ConsumerID not reset, got %q", order.ConsumerID)
	}
	if order.Provider != "" {
		t.Errorf("Provider not reset, got %q", order.Provider)
	}
	if order.Symbol != "" {
		t.Errorf("Symbol not reset, got %q", order.Symbol)
	}
	if order.Price != nil {
		t.Errorf("Price not reset, got %v", order.Price)
	}
	if order.Quantity != "" {
		t.Errorf("Quantity not reset, got %q", order.Quantity)
	}
}

func TestOrderRequestSetReturned(t *testing.T) {
	order := &OrderRequest{}
	
	if order.IsReturned() {
		t.Error("new order should not be returned")
	}
	
	order.SetReturned(true)
	if !order.IsReturned() {
		t.Error("order should be marked as returned")
	}
	
	order.SetReturned(false)
	if order.IsReturned() {
		t.Error("order should not be marked as returned")
	}
}

func TestOrderRequestNilHandling(t *testing.T) {
	var order *OrderRequest
	
	// Should not panic
	order.Reset()
	order.SetReturned(true)
	
	if order.IsReturned() {
		t.Error("nil order should return false for IsReturned")
	}
}
