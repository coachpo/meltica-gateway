package backtest

import (
	"context"
	"testing"

	"github.com/coachpo/meltica/internal/domain/schema"
)

func TestSimulatedExchange_SubmitOrder(t *testing.T) {
	strategy := loadTestStrategy(t, "noop")
	se := NewSimulatedExchange(strategy)

	price := "100"
	quantity := "1"
	ask := schema.OrderRequest{
		Symbol:    "BTC-USDT",
		Side:      schema.TradeSideSell,
		OrderType: schema.OrderTypeLimit,
		Price:     &price,
		Quantity:  quantity,
	}

	if _, err := se.SubmitOrder(context.Background(), ask); err != nil {
		t.Fatalf("SubmitOrder (ask) failed: %v", err)
	}

	buyPrice := "105"
	bid := schema.OrderRequest{
		Symbol:    "BTC-USDT",
		Side:      schema.TradeSideBuy,
		OrderType: schema.OrderTypeLimit,
		Price:     &buyPrice,
		Quantity:  quantity,
	}

	result, err := se.SubmitOrder(context.Background(), bid)
	if err != nil {
		t.Fatalf("SubmitOrder (bid) failed: %v", err)
	}
	if result.Report.FilledQuantity != quantity {
		t.Fatalf("expected filled quantity %s, got %s", quantity, result.Report.FilledQuantity)
	}
}
